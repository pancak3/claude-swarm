package tools

import (
	"context"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RoundDoneTool returns the swarm_round_done MCP tool.
// Sessions call this to signal they are finished with the current round,
// whether they submitted a proposal/vote or chose to abstain.
func RoundDoneTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_round_done",
		Description: "Signal that you are finished with the current round. Call this after submitting a proposal/critique/vote, or if you choose to abstain. The bus waits until all active sessions have signaled done before advancing to the next round.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Your session ID",
				},
				"auth_token": map[string]interface{}{
					"type":        "string",
					"description": "Auth token returned by swarm_register",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "What you did in this round: 'submitted' (filed a proposal/critique/rebuttal/vote) or 'abstain' (chose not to submit)",
					"enum":        []string{"submitted", "abstain"},
				},
			},
			"required": []string{"session_id", "auth_token", "action"},
		},
	}

	handler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := parseArgs(req.Params.Arguments)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
		}

		sessionID := getString(args, "session_id")
		authToken := getString(args, "auth_token")
		action := getString(args, "action")

		if msg := checkAuth(machine.SessionRegistry, sessionID, authToken); msg != "" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
		}

		machine.SessionRegistry.Heartbeat(sessionID)

		firstTime := machine.MarkSessionDone(sessionID)
		active := machine.SessionRegistry.ActiveCount()
		done := machine.DoneSessionCount()

		msg := fmt.Sprintf("Session %q marked as done (action=%s). %d/%d active sessions done.",
			sessionID, action, done, active)

		if !firstTime {
			msg = fmt.Sprintf("Session %q was already marked as done. %d/%d active sessions done.",
				sessionID, done, active)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		}, nil
	}

	return tool, handler
}
