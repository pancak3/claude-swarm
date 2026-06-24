package bus_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
)

func TestSubmitCritiqueRejectsNonexistentProposal(t *testing.T) {
	m := setupMachine()
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()

	c := &protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "nonexistent", Strengths: []string{"x"}, Weaknesses: []string{"y"}}
	err := m.SubmitCritique(c)
	if err == nil {
		t.Error("expected error submitting critique for nonexistent proposal")
	} else if fmt.Sprintf("%v", err) != "critique references non-existent proposal: target proposal \"nonexistent\" not found" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSubmitVoteRejectsNonexistentProposal(t *testing.T) {
	m := setupMachine()
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()

	v := &protocol.Vote{ID: "v1", SessionID: "s1", RankedVotes: []string{"p1", "nonexistent"}}
	err := m.SubmitVote(v)
	if err == nil {
		t.Error("expected error voting for nonexistent proposal")
	}
}

func TestSubmitVoteRejectsEliminatedProposal(t *testing.T) {
	m := setupMachine()
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.SubmitProposal(&protocol.Proposal{ID: "p2", SessionID: "s2", Approach: "A2", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	// p3 owned by s3 — s2 and s1 can critique it
	m.SubmitProposal(&protocol.Proposal{ID: "p3", SessionID: "s3", Approach: "A3", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})

	m.RoundManager.Advance()

	ff := "fatal"
	m.SubmitCritique(&protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "p3", Weaknesses: []string{"x"}, FatalFlaw: &ff})
	m.SubmitCritique(&protocol.Critique{ID: "c2", SessionID: "s1", TargetProposalID: "p3", Weaknesses: []string{"x"}, FatalFlaw: &ff})

	m.EliminateByFatalFlaw(0.5)
	active := m.ActiveProposalIDs()
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %v", active)
	}

	m.RoundManager.Advance()
	m.RoundManager.Advance()

	v := &protocol.Vote{ID: "v1", SessionID: "s1", RankedVotes: []string{"p1", "p3"}}
	err := m.SubmitVote(v)
	if err == nil {
		t.Error("expected error voting for eliminated proposal")
	}
}

func TestCountFatalFlawsWithPartialSessionParticipation(t *testing.T) {
	ff := "fatal"
	critiques := []protocol.Critique{
		{ID: "c1", SessionID: "s1", TargetProposalID: "p1", FatalFlaw: &ff},
		{ID: "c2", SessionID: "s2", TargetProposalID: "p1", FatalFlaw: &ff},
	}

	eliminated := protocol.CountFatalFlaws(critiques, 0.8)
	if len(eliminated) != 1 || eliminated[0] != "p1" {
		t.Errorf("expected p1 eliminated (2/2 critiquing sessions flagged), got %v", eliminated)
	}
}

func TestCountFatalFlawsDenominatorDifference(t *testing.T) {
	ff := "fatal"
	critiques := []protocol.Critique{
		{ID: "c1", SessionID: "s2", TargetProposalID: "p1", FatalFlaw: &ff},
		{ID: "c2", SessionID: "s3", TargetProposalID: "p2"},
	}

	eliminated := protocol.CountFatalFlaws(critiques, 0.5)
	found := false
	for _, pid := range eliminated {
		if pid == "p1" {
			found = true
		}
	}
	if !found {
		t.Error("expected p1 eliminated: 1/2 critiquing sessions flagged fatal flaw >= 0.5 threshold")
	}

	// Single session critiquing — threshold 0.5 gives cutoff=1, so 1 fatal flaw eliminates.
	critiques2 := []protocol.Critique{
		{ID: "c3", SessionID: "s2", TargetProposalID: "p1", FatalFlaw: &ff},
	}
	eliminated2 := protocol.CountFatalFlaws(critiques2, 0.5)
	if len(eliminated2) != 0 {
		t.Errorf("expected no elimination with only 1 critiquing session (denominator min 2), got %v", eliminated2)
	}
}

func TestSubmitCritiqueValidatesTargetInProposeRound(t *testing.T) {
	m := setupMachine()
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{ID: "p1", SessionID: "s1", Approach: "A1", Architecture: "a", Risks: []string{}, Subtasks: 1, Confidence: 50})
	m.RoundManager.Advance()

	err := m.SubmitCritique(&protocol.Critique{ID: "c1", SessionID: "s2", TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{"bad"}})
	if err != nil {
		t.Fatalf("valid critique should succeed: %v", err)
	}
}

func TestFatalFlawWithNoCritiques(t *testing.T) {
	m := setupMachine()
	eliminated := m.EliminateByFatalFlaw(0.5)
	if len(eliminated) != 0 {
		t.Errorf("expected no elimination with no critiques, got %v", eliminated)
	}
}

func setupMachine() *state.Machine {
	brief := &protocol.TaskBrief{TaskID: "test-cross", Description: "cross validation test", SwarmSize: 3}
	m := state.NewMachine("test-cross", brief, testTimeouts(), 30*time.Second)
	m.RoundManager.SetFastPath(false)
	m.SessionRegistry.Register("s1", "correctness")
	m.SessionRegistry.Register("s2", "simplicity")
	m.SessionRegistry.Register("s3", "performance")
	return m
}
