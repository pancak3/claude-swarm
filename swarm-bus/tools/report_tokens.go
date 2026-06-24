package tools

import (
	"context"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReportTokensTool returns the swarm_report_tokens MCP tool.
func ReportTokensTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_report_tokens",
		Description: "Report token usage for your session (tokens_in consumed, tokens_out generated).",
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
				"tokens_in": map[string]interface{}{
					"type":        "integer",
					"description": "Input tokens consumed",
				},
				"tokens_out": map[string]interface{}{
					"type":        "integer",
					"description": "Output tokens generated",
				},
			},
			"required": []string{"session_id", "auth_token", "tokens_in", "tokens_out"},
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

		tokensIn := int64(getInt(args, "tokens_in"))
		tokensOut := int64(getInt(args, "tokens_out"))

		if !machine.SessionRegistry.UpdateTokens(sessionID, tokensIn, tokensOut) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("session %q not found", sessionID)}},
			}, nil
		}

		machine.SessionRegistry.Heartbeat(sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Token usage reported for session %q: +%d in, +%d out", sessionID, tokensIn, tokensOut),
			}},
		}, nil
	}

	return tool, handler
}
