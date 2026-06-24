package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SubmitProposalTool returns the swarm_submit_proposal MCP tool.
func SubmitProposalTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_submit_proposal",
		Description: "Submit your solution proposal during the PROPOSE round. Include your approach, architecture, risks, estimated sub-tasks, and confidence level (0-100).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type": "string", "description": "Your session ID (from swarm_register)",
				},
				"auth_token": map[string]interface{}{
					"type": "string", "description": "Auth token returned by swarm_register",
				},
				"approach": map[string]interface{}{
					"type": "string", "description": "High-level description of your proposed approach",
				},
				"architecture": map[string]interface{}{
					"type": "string", "description": "Technical architecture details",
				},
				"risks": map[string]interface{}{
					"type": "array", "items": map[string]interface{}{"type": "string"},
					"description": "Identified risks and mitigations",
				},
				"estimated_subtasks": map[string]interface{}{
					"type": "integer", "description": "Estimated number of sub-tasks to complete this work",
				},
				"confidence": map[string]interface{}{
					"type": "integer", "description": "Confidence level (0-100) in this proposal",
				},
			},
			"required": []string{"session_id", "auth_token", "approach", "architecture", "risks", "estimated_subtasks", "confidence"},
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

		proposal := &protocol.Proposal{
			ID:           genID("p"),
			SessionID:    sessionID,
			Approach:     getString(args, "approach"),
			Architecture: getString(args, "architecture"),
			Risks:        getStringSlice(args, "risks"),
			Subtasks:     getInt(args, "estimated_subtasks"),
			Confidence:   getInt(args, "confidence"),
			Timestamp:    time.Now(),
		}

		if err := protocol.ValidateProposal(proposal); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid proposal: %v", err)}}}, nil
		}
		if err := machine.SubmitProposal(proposal); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("submission failed: %v", err)}}}, nil
		}

		machine.SessionRegistry.Heartbeat(proposal.SessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Proposal %q submitted. %d/%d sessions have proposed.",
					proposal.ID, machine.SubmissionCount(), machine.SessionRegistry.ActiveCount()),
			}},
		}, nil
	}

	return tool, handler
}
