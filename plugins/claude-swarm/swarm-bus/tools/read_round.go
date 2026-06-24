package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReadRoundTool returns the swarm_read_round MCP tool.
func ReadRoundTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_read_round",
		Description: "Read submissions for the current round. PROPOSE: all proposals (anonymized). CRITIQUE: all proposals. REBUTTAL: critiques of YOUR proposal. VOTE: surviving proposal IDs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "Your session ID"},
				"auth_token": map[string]interface{}{"type": "string", "description": "Auth token returned by swarm_register"},
			},
			"required": []string{"session_id", "auth_token"},
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

		currentRound := machine.RoundManager.Current()

		var data interface{}
		switch currentRound {
		case "PROPOSE", "CRITIQUE":
			data = machine.GetProposals(true, sessionID)
		case "REBUTTAL":
			ownProposals := machine.GetProposals(false, sessionID)
			ownID := ""
			for _, p := range ownProposals {
				if p.SessionID == sessionID {
					ownID = p.ID
					break
				}
			}
			if ownID != "" {
				data = machine.GetCritiquesForProposal(ownID)
			} else {
				data = []string{}
			}
		case "VOTE":
			data = machine.ActiveProposalIDs()
		default:
			data = map[string]string{"message": fmt.Sprintf("No data to read in %s round", currentRound)}
		}

		jsonData, _ := json.MarshalIndent(data, "", "  ")
		machine.SessionRegistry.Heartbeat(sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(jsonData)}},
		}, nil
	}

	return tool, handler
}
