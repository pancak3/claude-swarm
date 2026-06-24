package tools

import (
	"context"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SubmitRebuttalTool returns the swarm_submit_rebuttal MCP tool.
func SubmitRebuttalTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_submit_rebuttal",
		Description: "Submit your rebuttal to critiques of your proposal during the REBUTTAL round. For each critique point, respond with agree, concede, or defend.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "Your session ID"},
				"auth_token": map[string]interface{}{"type": "string", "description": "Auth token returned by swarm_register"},
				"rebuttals": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"critique_point":   map[string]interface{}{"type": "string", "description": "The critique point you are responding to"},
							"response":         map[string]interface{}{"type": "string", "description": "agree, concede, or defend"},
							"amended_approach": map[string]interface{}{"type": "string", "description": "If agree: your amended approach"},
							"reasoning":        map[string]interface{}{"type": "string", "description": "If defend: your reasoning"},
						},
						"required": []string{"critique_point", "response"},
					},
					"description": "Array of rebuttal responses",
				},
			},
			"required": []string{"session_id", "auth_token", "rebuttals"},
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

		rebuttalsRaw, _ := args["rebuttals"].([]interface{})

		responses := make([]protocol.RebuttalResponse, 0, len(rebuttalsRaw))
		for _, raw := range rebuttalsRaw {
			rMap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			responses = append(responses, protocol.RebuttalResponse{
				CritiquePoint:   getString(rMap, "critique_point"),
				Response:        getString(rMap, "response"),
				AmendedApproach: getString(rMap, "amended_approach"),
				Reasoning:       getString(rMap, "reasoning"),
			})
		}

		r := &protocol.Rebuttal{
			ID:        genID("r"),
			SessionID: sessionID,
			Responses: responses,
			Timestamp: time.Now(),
		}

		if err := protocol.ValidateRebuttal(r); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "invalid rebuttal: " + err.Error()}}}, nil
		}
		if err := machine.SubmitRebuttal(r); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "submission failed: " + err.Error()}}}, nil
		}

		machine.SessionRegistry.Heartbeat(sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Rebuttal submitted."}},
		}, nil
	}

	return tool, handler
}
