package tools

import (
	"context"
	"fmt"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterContractTool returns the swarm_register_contract MCP tool.
func RegisterContractTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_register_contract",
		Description: "Register module/class names in the shared contract. Prevents naming conflicts across sessions.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "Your session ID"},
				"auth_token": map[string]interface{}{"type": "string", "description": "Auth token from swarm_register"},
				"module_name": map[string]interface{}{"type": "string", "description": "Your module/file name (e.g. mobility_predictor)"},
				"class_name":  map[string]interface{}{"type": "string", "description": "Your class name (e.g. MobilityPredictor)"},
				"description": map[string]interface{}{"type": "string", "description": "Brief purpose of this module"},
			},
			"required": []string{"session_id", "auth_token", "module_name", "class_name"},
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

		entry := protocol.ContractEntry{
			SessionID:   sessionID,
			ModuleName:  getString(args, "module_name"),
			ClassName:   getString(args, "class_name"),
			Description: getString(args, "description"),
		}
		machine.RegisterContract(entry)

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Contract registered: module=%q class=%q", entry.ModuleName, entry.ClassName),
			}},
		}, nil
	}

	return tool, handler
}
