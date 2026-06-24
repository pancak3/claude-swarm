package tools

import (
	"context"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterTool returns the swarm_register MCP tool.
func RegisterTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_register",
		Description: "Register this session with the swarm bus. Must be called first, before any other swarm_* tool.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Unique session identifier assigned by the orchestrator",
				},
				"perspective": map[string]interface{}{
					"type":        "string",
					"description": "The diversity perspective assigned to this session (correctness, simplicity, performance, security)",
				},
			},
			"required": []string{"session_id", "perspective"},
		},
	}

	handler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := parseArgs(req.Params.Arguments)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
		}

		sessionID := getString(args, "session_id")
		perspective := getString(args, "perspective")

		if sessionID == "" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "session_id is required"}}}, nil
		}

		info, token, err := machine.SessionRegistry.Register(sessionID, perspective)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("registration failed: %v", err)}}}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Registered as session %q. Auth token: %s. Task: %s. Swarm size: %d. Round: %s.",
					info.ID, token, machine.TaskBrief.Description, machine.TaskBrief.SwarmSize,
					machine.RoundManager.Current()),
			}},
		}, nil
	}

	return tool, handler
}
