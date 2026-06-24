package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CastVoteTool returns the swarm_cast_vote MCP tool.
func CastVoteTool(machine *state.Machine) (*mcp.Tool, mcp.ToolHandler) {
	tool := &mcp.Tool{
		Name:        "swarm_cast_vote",
		Description: "Cast your ranked-choice vote during the VOTE round. List proposal IDs in order of preference (most preferred first).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "Your session ID"},
				"auth_token": map[string]interface{}{"type": "string", "description": "Auth token returned by swarm_register"},
				"ranked_votes": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Proposal IDs in order of preference (most preferred first)",
				},
			},
			"required": []string{"session_id", "auth_token", "ranked_votes"},
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

		v := &protocol.Vote{
			ID:          genID("v"),
			SessionID:   sessionID,
			RankedVotes: getStringSlice(args, "ranked_votes"),
			Timestamp:   time.Now(),
		}

		if err := protocol.ValidateVote(v); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "invalid vote: " + err.Error()}}}, nil
		}
		if err := machine.SubmitVote(v); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "submission failed: " + err.Error()}}}, nil
		}

		machine.SessionRegistry.Heartbeat(v.SessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Vote cast. %d/%d sessions have voted.",
					machine.SubmissionCount(), machine.SessionRegistry.ActiveCount()),
			}},
		}, nil
	}

	return tool, handler
}
