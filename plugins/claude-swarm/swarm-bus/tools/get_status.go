package tools

import (
	"context"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetStatusTool returns the swarm_get_status MCP tool.
func GetStatusTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_get_status",
		Description: "Get current swarm status: round, time remaining, active session count, submission count.",
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

		machine.SessionRegistry.Heartbeat(sessionID)
		status := fmt.Sprintf(
			"Task: %s\nRound: %s\nTime remaining: %s\nSessions active: %d\nSubmissions received: %d\nDeadlock retries: %d",
			machine.TaskID,
			machine.RoundManager.Current(),
			machine.RoundManager.TimeRemaining(),
			machine.SessionRegistry.ActiveCount(),
			machine.SubmissionCount(),
			machine.GetDeadlockRetries(),
		)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: status}},
		}, nil
	}

	return tool, handler
}
