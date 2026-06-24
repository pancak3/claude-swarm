package bus_test

import (
	"testing"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
)

func testTimeouts() protocol.RoundTimeouts {
	return protocol.RoundTimeouts{
		Register: 30 * time.Second,
		Propose:  60 * time.Second,
		Critique: 90 * time.Second,
		Rebuttal: 60 * time.Second,
		Vote:     30 * time.Second,
	}
}

func TestMachineStartsInRegistering(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-1", Description: "test task", SwarmSize: 3}
	m := state.NewMachine("test-1", brief, testTimeouts(), 30*time.Second)

	if m.RoundManager.Current() != protocol.RoundRegistering {
		t.Errorf("expected REGISTERING, got %s", m.RoundManager.Current())
	}
}

func TestSessionRegistration(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-2", Description: "test task", SwarmSize: 3}
	m := state.NewMachine("test-2", brief, testTimeouts(), 30*time.Second)

	_, _, err := m.SessionRegistry.Register("s1", "correctness")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if m.SessionRegistry.ActiveCount() != 1 {
		t.Errorf("expected 1 active session, got %d", m.SessionRegistry.ActiveCount())
	}

	_, _, err = m.SessionRegistry.Register("s1", "correctness")
	if err == nil {
		t.Error("expected duplicate registration error")
	}
}

func TestSubmitProposalInWrongRound(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-3", Description: "test task", SwarmSize: 3}
	m := state.NewMachine("test-3", brief, testTimeouts(), 30*time.Second)

	p := &protocol.Proposal{
		ID: "p1", SessionID: "s1", Approach: "test approach",
		Architecture: "test arch", Risks: []string{"risk1"},
		Subtasks: 3, Confidence: 80,
	}

	err := m.SubmitProposal(p)
	if err == nil {
		t.Error("expected error submitting proposal in REGISTERING round")
	}
}

func TestProposeThenCritiqueFlow(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-4", Description: "test", SwarmSize: 3}
	m := state.NewMachine("test-4", brief, testTimeouts(), 30*time.Second)

	_, _, _ = m.SessionRegistry.Register("s1", "correctness")
	_, _, _ = m.SessionRegistry.Register("s2", "simplicity")

	m.RoundManager.Advance()
	if m.RoundManager.Current() != protocol.RoundPropose {
		t.Fatalf("expected PROPOSE, got %s", m.RoundManager.Current())
	}

	p1 := &protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "arch1", Risks: []string{}, Subtasks: 2, Confidence: 70}
	p2 := &protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "arch2", Risks: []string{}, Subtasks: 3, Confidence: 80}

	if err := m.SubmitProposal(p1); err != nil {
		t.Fatalf("submit p1 failed: %v", err)
	}
	if err := m.SubmitProposal(p2); err != nil {
		t.Fatalf("submit p2 failed: %v", err)
	}

	proposals := m.GetProposals(true, "s1")
	if len(proposals) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(proposals))
	}
	for _, p := range proposals {
		if p.SessionID == "" {
			continue
		}
		if p.SessionID != "s1" {
			t.Errorf("expected only s1's proposal un-anonymized, got session_id=%q", p.SessionID)
		}
	}

	m.RoundManager.Advance()
	if m.RoundManager.Current() != protocol.RoundCritique {
		t.Fatalf("expected CRITIQUE, got %s", m.RoundManager.Current())
	}

	ff := "fatal: doesn't work"
	c1 := &protocol.Critique{ID: "c1", SessionID: "s1", TargetProposalID: "p2", Strengths: []string{"good"}, Weaknesses: []string{"bad"}, FatalFlaw: &ff}
	if err := m.SubmitCritique(c1); err != nil {
		t.Fatalf("submit critique failed: %v", err)
	}
}

func TestFatalFlawElimination(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-5", Description: "test", SwarmSize: 3}
	m := state.NewMachine("test-5", brief, testTimeouts(), 30*time.Second)

	_, _, _ = m.SessionRegistry.Register("s1", "correctness")
	_, _, _ = m.SessionRegistry.Register("s2", "simplicity")
	_, _, _ = m.SessionRegistry.Register("s3", "performance")

	m.RoundManager.Advance()

	_ = m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	_ = m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})

	m.RoundManager.Advance()

	ff := "fatal flaw"
	_ = m.SubmitCritique(&protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "p1", Strengths: []string{}, Weaknesses: []string{"x"}, FatalFlaw: &ff})
	_ = m.SubmitCritique(&protocol.Critique{ID: "c2", SessionID: "s3", TargetProposalID: "p1", Strengths: []string{}, Weaknesses: []string{"x"}, FatalFlaw: &ff})

	eliminated := m.EliminateByFatalFlaw(0.5)
	if len(eliminated) != 1 || eliminated[0] != "p1" {
		t.Errorf("expected p1 eliminated, got %v", eliminated)
	}

	active := m.ActiveProposalIDs()
	if len(active) != 1 || active[0] != "p2" {
		t.Errorf("expected only p2 active, got %v", active)
	}
}

func TestFastPathRoundTransition(t *testing.T) {
	brief := &protocol.TaskBrief{TaskID: "test-6", Description: "test fast path", SwarmSize: 2}
	m := state.NewMachine("test-6", brief, testTimeouts(), 30*time.Second)

	m.RoundManager.SetFastPath(true)
	_, _, _ = m.SessionRegistry.Register("s1", "correctness")
	_, _, _ = m.SessionRegistry.Register("s2", "simplicity")

	m.RoundManager.Advance()
	if m.RoundManager.Current() != protocol.RoundPropose {
		t.Fatalf("expected PROPOSE, got %s", m.RoundManager.Current())
	}

	m.RoundManager.Advance()
	if m.RoundManager.Current() != protocol.RoundVote {
		t.Fatalf("expected VOTE (fast path skipped critique), got %s", m.RoundManager.Current())
	}

	m.RoundManager.Advance()
	if m.RoundManager.Current() != protocol.RoundExecute {
		t.Fatalf("expected EXECUTE, got %s", m.RoundManager.Current())
	}
}
