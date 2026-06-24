package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestMachine(swarmSize int) *state.Machine {
	brief := &protocol.TaskBrief{
		TaskID:      "test-tools",
		Description: "tools integration test",
		SwarmSize:   swarmSize,
	}
	timeouts := protocol.RoundTimeouts{
		Register: 30 * time.Second,
		Propose:  30 * time.Second,
		Critique: 30 * time.Second,
		Rebuttal: 30 * time.Second,
		Vote:     30 * time.Second,
	}
	m := state.NewMachine("test-tools", brief, timeouts, 30*time.Second)
	m.RoundManager.SetFastPath(false)
	return m
}

func makeCallRequest(args map[string]interface{}) *mcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func resetRateLimiter() *rateLimiter {
	old := globalRateLimiter
	globalRateLimiter = newRateLimiter(defaultRateLimiterRate, defaultRateLimiterBurst)
	return old
}

func TestRegisterToolHandlerValid(t *testing.T) {
	m := newTestMachine(3)
	_, handler := RegisterTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "perspective": "correctness",
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestRegisterToolHandlerMissingSessionID(t *testing.T) {
	m := newTestMachine(3)
	_, handler := RegisterTool(m)
	req := makeCallRequest(map[string]interface{}{"perspective": "correctness"})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestRegisterToolHandlerDuplicateSession(t *testing.T) {
	m := newTestMachine(3)
	_, handler := RegisterTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "perspective": "correctness"})
	handler(context.Background(), req)
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for duplicate session")
	}
}

func TestRegisterToolHandlerBadArgs(t *testing.T) {
	m := newTestMachine(3)
	_, handler := RegisterTool(m)
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{invalid}`)}}
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for bad args")
	}
}

func TestSubmitProposalToolValid(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	_, handler := SubmitProposalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"approach": "test approach", "architecture": "test arch",
		"risks": []interface{}{"risk1"}, "estimated_subtasks": float64(3), "confidence": float64(80),
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestSubmitProposalToolInvalidAuth(t *testing.T) {
	m := newTestMachine(3)
	m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	_, handler := SubmitProposalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": "wrong-token",
		"approach": "test", "architecture": "arch", "risks": []interface{}{},
		"estimated_subtasks": float64(1), "confidence": float64(50),
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected auth error")
	}
}

func TestSubmitProposalToolRateLimited(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	oldRL := globalRateLimiter
	globalRateLimiter = newRateLimiter(0, 0)
	defer func() { globalRateLimiter = oldRL }()
	_, handler := SubmitProposalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"approach": "test", "architecture": "arch", "risks": []interface{}{},
		"estimated_subtasks": float64(1), "confidence": float64(50),
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected rate limit error")
	}
}

func TestSubmitProposalToolWrongRound(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := SubmitProposalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"approach": "test", "architecture": "arch", "risks": []interface{}{},
		"estimated_subtasks": float64(1), "confidence": float64(50),
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected wrong round error")
	}
}

func TestSubmitProposalToolInvalidApproach(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	_, handler := SubmitProposalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"approach": "", "architecture": "arch", "risks": []interface{}{},
		"estimated_subtasks": float64(1), "confidence": float64(50),
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected validation error for empty approach")
	}
}

func setupCritiqueTest(t *testing.T) (*state.Machine, map[string]string) {
	t.Helper()
	m := newTestMachine(3)
	m.RoundManager.SetFastPath(false)
	_, t1, _ := m.SessionRegistry.Register("s1", "correctness")
	_, t2, _ := m.SessionRegistry.Register("s2", "simplicity")
	tokens := map[string]string{"s1": t1, "s2": t2}
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()
	return m, tokens
}

func TestSubmitCritiqueToolValid(t *testing.T) {
	defer resetRateLimiter()
	m, tokens := setupCritiqueTest(t)
	_, handler := SubmitCritiqueTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s2", "auth_token": tokens["s2"],
		"critiques": []interface{}{map[string]interface{}{
			"target_proposal_id": "p1",
			"strengths": []interface{}{"good"}, "weaknesses": []interface{}{"bad"},
		}},
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestSubmitCritiqueToolWithFatalFlaw(t *testing.T) {
	defer resetRateLimiter()
	m, tokens := setupCritiqueTest(t)
	_, handler := SubmitCritiqueTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s2", "auth_token": tokens["s2"],
		"critiques": []interface{}{map[string]interface{}{
			"target_proposal_id": "p1",
			"strengths": []interface{}{"good"}, "weaknesses": []interface{}{"bad"},
			"fatal_flaw": "doesn't work",
		}},
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestSubmitCritiqueToolInvalidTarget(t *testing.T) {
	defer resetRateLimiter()
	m, tokens := setupCritiqueTest(t)
	_, handler := SubmitCritiqueTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s2", "auth_token": tokens["s2"],
		"critiques": []interface{}{map[string]interface{}{
			"target_proposal_id": "nonexistent",
			"strengths": []interface{}{"good"}, "weaknesses": []interface{}{"bad"},
		}},
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for nonexistent target")
	}
}

func setupRebuttalTest(t *testing.T) (*state.Machine, map[string]string) {
	t.Helper()
	m := newTestMachine(3)
	m.RoundManager.SetFastPath(false)
	_, t1, _ := m.SessionRegistry.Register("s1", "correctness")
	_, t2, _ := m.SessionRegistry.Register("s2", "simplicity")
	tokens := map[string]string{"s1": t1, "s2": t2}
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()
	m.SubmitCritique(&protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{"bad"}})
	m.RoundManager.Advance()
	return m, tokens
}

func TestSubmitRebuttalToolValid(t *testing.T) {
	defer resetRateLimiter()
	m, tokens := setupRebuttalTest(t)
	_, handler := SubmitRebuttalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": tokens["s1"],
		"rebuttals": []interface{}{map[string]interface{}{
			"critique_point": "bad", "response": "defend",
			"reasoning": "our approach is actually good",
		}},
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestSubmitRebuttalToolInvalidResponse(t *testing.T) {
	m, tokens := setupRebuttalTest(t)
	_, handler := SubmitRebuttalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": tokens["s1"],
		"rebuttals": []interface{}{map[string]interface{}{
			"critique_point": "bad", "response": "reject",
		}},
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for invalid response type")
	}
}

func TestSubmitRebuttalToolWrongRound(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := SubmitRebuttalTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"rebuttals": []interface{}{map[string]interface{}{
			"critique_point": "x", "response": "defend",
		}},
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected wrong round error")
	}
}

func setupVoteTest(t *testing.T) (*state.Machine, map[string]string) {
	t.Helper()
	m := newTestMachine(3)
	m.RoundManager.SetFastPath(true)
	_, t1, _ := m.SessionRegistry.Register("s1", "correctness")
	_, t2, _ := m.SessionRegistry.Register("s2", "simplicity")
	tokens := map[string]string{"s1": t1, "s2": t2}
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()
	return m, tokens
}

func TestCastVoteToolValid(t *testing.T) {
	defer resetRateLimiter()
	m, tokens := setupVoteTest(t)
	_, handler := CastVoteTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": tokens["s1"],
		"ranked_votes": []interface{}{"p2"},
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestCastVoteToolSelfVote(t *testing.T) {
	m, tokens := setupVoteTest(t)
	_, handler := CastVoteTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": tokens["s1"],
		"ranked_votes": []interface{}{"p1", "p2"},
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected self-vote error")
	}
}

func TestCastVoteToolEmptyVote(t *testing.T) {
	m, tokens := setupVoteTest(t)
	_, handler := CastVoteTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": tokens["s1"],
		"ranked_votes": []interface{}{},
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected empty vote error")
	}
}

func TestReadRoundToolInPropose(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	_, handler := ReadRoundTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestReadRoundToolInVote(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()
	_, handler := ReadRoundTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestReadRoundToolInRegistering(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := ReadRoundTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestGetStatusToolValid(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := GetStatusTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestGetStatusToolInvalidAuth(t *testing.T) {
	m := newTestMachine(3)
	_, handler := GetStatusTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "nonexistent", "auth_token": "bad"})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected auth error")
	}
}

func TestRegisterContractToolValid(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := RegisterContractTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"module_name": "validator", "class_name": "InputValidator", "description": "validates inputs",
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	if len(m.GetContracts()) != 1 {
		t.Errorf("expected 1 contract entry, got %d", len(m.GetContracts()))
	}
}

func TestGetContractToolEmpty(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := GetContractTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestGetContractToolWithEntries(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	m.AddContractEntry("s1", "mod", "Class", "desc")
	_, handler := GetContractTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": token})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestReportTokensToolValid(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := ReportTokensTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token,
		"tokens_in": float64(1000), "tokens_out": float64(500),
	})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	si, _ := m.SessionRegistry.GetInfo("s1")
	if si.TokensIn != 1000 || si.TokensOut != 500 {
		t.Errorf("token counts not updated: in=%d out=%d", si.TokensIn, si.TokensOut)
	}
}

func TestReportTokensToolNonexistentSession(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := ReportTokensTool(m)
	req := makeCallRequest(map[string]interface{}{
		"session_id": "nonexistent", "auth_token": token,
		"tokens_in": float64(100), "tokens_out": float64(50),
	})
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestReportTokensToolAccumulates(t *testing.T) {
	m := newTestMachine(3)
	_, token, _ := m.SessionRegistry.Register("s1", "correctness")
	_, handler := ReportTokensTool(m)
	handler(context.Background(), makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token, "tokens_in": float64(1000), "tokens_out": float64(500),
	}))
	handler(context.Background(), makeCallRequest(map[string]interface{}{
		"session_id": "s1", "auth_token": token, "tokens_in": float64(500), "tokens_out": float64(300),
	}))
	si, _ := m.SessionRegistry.GetInfo("s1")
	if si.TokensIn != 1500 || si.TokensOut != 800 {
		t.Errorf("token counts should accumulate: in=%d out=%d", si.TokensIn, si.TokensOut)
	}
}

func TestReadRoundToolInRebuttal(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	m.RoundManager.SetFastPath(false)
	_, t1, _ := m.SessionRegistry.Register("s1", "correctness")
	_, _, _ = m.SessionRegistry.Register("s2", "simplicity")
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()
	m.SubmitCritique(&protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{"bad"}})
	m.RoundManager.Advance()
	_, handler := ReadRoundTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s1", "auth_token": t1})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}

func TestReadRoundToolInRebuttalNoOwnProposal(t *testing.T) {
	defer resetRateLimiter()
	m := newTestMachine(3)
	m.RoundManager.SetFastPath(false)
	_, t3, _ := m.SessionRegistry.Register("s3", "performance")
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.ForceRound(protocol.RoundRebuttal)
	_, handler := ReadRoundTool(m)
	req := makeCallRequest(map[string]interface{}{"session_id": "s3", "auth_token": t3})
	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}
