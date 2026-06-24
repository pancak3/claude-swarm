package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SubmitCritiqueTool returns the swarm_submit_critique MCP tool.
func SubmitCritiqueTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_submit_critique",
		Description: "Submit your critique of proposals during the CRITIQUE round. Provide strengths, weaknesses, and optionally flag a fatal flaw. Critique every proposal you read.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type": "string", "description": "Your session ID",
				},
				"auth_token": map[string]interface{}{
					"type": "string", "description": "Auth token returned by swarm_register",
				},
				"critiques": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"target_proposal_id": map[string]interface{}{"type": "string", "description": "ID of the proposal being critiqued"},
							"strengths":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"weaknesses":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"fatal_flaw":         map[string]interface{}{"type": "string", "description": "If the proposal has a fatal flaw, describe it here"},
						},
						"required": []string{"target_proposal_id", "strengths", "weaknesses"},
					},
					"description": "Array of critiques, one per proposal",
				},
			},
			"required": []string{"session_id", "auth_token", "critiques"},
		},
	}

	handler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := parseArgs(req.Params.Arguments)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
		}

		sessionID := getString(args, "session_id")
		authToken := getString(args, "auth_token")

		if msg := checkAuth(machine.SessionRegistry, sessionID, authToken); msg != "" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
		}
		if !globalRateLimiter.Allow(sessionID) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "rate limit exceeded: too many requests"}}}, nil
		}

		critiquesRaw, _ := args["critiques"].([]interface{})

		submitted := 0
		for _, raw := range critiquesRaw {
			cMap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			var fatalFlaw *string
			if ff := getString(cMap, "fatal_flaw"); ff != "" {
				fatalFlaw = &ff
			}
			c := &protocol.Critique{
				ID:               genID("c"),
				SessionID:        sessionID,
				TargetProposalID: getString(cMap, "target_proposal_id"),
				Strengths:        getStringSlice(cMap, "strengths"),
				Weaknesses:       getStringSlice(cMap, "weaknesses"),
				FatalFlaw:        fatalFlaw,
				Timestamp:        time.Now(),
			}
			if err := protocol.ValidateCritique(c); err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid critique for %s: %v", c.TargetProposalID, err)}}}, nil
			}
			if err := machine.SubmitCritique(c); err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("submission failed: %v", err)}}}, nil
			}
			submitted++
		}

		machine.SessionRegistry.Heartbeat(sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%d critiques submitted.", submitted)}},
		}, nil
	}

	return tool, handler
}
