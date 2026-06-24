package state

import (
	"testing"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
)

func TestNewMachine(t *testing.T) {
	m := newTestMachine()
	if m == nil {
		t.Fatal("NewMachine returned nil")
	}
	if m.TaskID != "test-task" {
		t.Errorf("TaskID = %q, want %q", m.TaskID, "test-task")
	}
	if m.RoundManager.Current() != protocol.RoundRegistering {
		t.Errorf("initial round = %v, want %v", m.RoundManager.Current(), protocol.RoundRegistering)
	}
}

func TestSessionRegistration(t *testing.T) {
	m := newTestMachine()
	sess, _, err := m.SessionRegistry.Register("s1", "correctness")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if sess == nil || sess.ID != "s1" {
		t.Errorf("session ID = %v, want s1", sess)
	}
	if m.SessionRegistry.ActiveCount() != 1 {
		t.Errorf("active count = %d, want 1", m.SessionRegistry.ActiveCount())
	}
}

func TestDuplicateRegistration(t *testing.T) {
	m := newTestMachine()
	_, _, _ = m.SessionRegistry.Register("s1", "correctness")
	_, _, err := m.SessionRegistry.Register("s1", "simplicity")
	if err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestSubmitProposal(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()

	prop := &protocol.Proposal{
		SessionID:    "s1",
		Approach:     "test approach",
		Architecture: "test architecture",
		Confidence:   85,
	}
	err := m.SubmitProposal(prop)
	if err != nil {
		t.Fatalf("SubmitProposal failed: %v", err)
	}
	if len(m.ActiveProposalIDs()) != 1 {
		t.Errorf("active proposals = %d, want 1", len(m.ActiveProposalIDs()))
	}
}

func TestSubmitProposalWrongRound(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	// Still in REGISTERING — proposal should be rejected
	prop := &protocol.Proposal{
		SessionID:    "s1",
		Approach:     "test approach",
		Architecture: "test architecture",
		Confidence:   85,
	}
	err := m.SubmitProposal(prop)
	if err == nil {
		t.Error("expected error submitting proposal in wrong round")
	}
}

func TestGetProposalsEmpty(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()

	all := m.GetProposals(false, "")
	if len(all) != 0 {
		t.Errorf("GetProposals returned %d, want 0 before any submission", len(all))
	}
}


func TestStatusSnapshot(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.SessionRegistry.Register("s2", "simplicity")

	snap := m.StatusSnapshot()
	if snap.Round != protocol.RoundRegistering {
		t.Errorf("round = %v, want %v", snap.Round, protocol.RoundRegistering)
	}
	if snap.ActiveSessions != 2 {
		t.Errorf("active = %d, want 2", snap.ActiveSessions)
	}
	if snap.TotalSessions != 8 {
		t.Errorf("total = %d, want 8", snap.TotalSessions)
	}
}

func TestEliminateProposal(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.SessionRegistry.Register("s2", "simplicity")
	m.RoundManager.Advance()

	m.SubmitProposal(&protocol.Proposal{SessionID: "s1", Approach: "a", Architecture: "b", Confidence: 80})
	m.SubmitProposal(&protocol.Proposal{SessionID: "s2", Approach: "c", Architecture: "d", Confidence: 90})

	eliminated := m.GetEliminatedProposals()
	if len(eliminated) != 0 {
		t.Errorf("eliminated = %d, want 0", len(eliminated))
	}
}

func TestSelfVotePrevention(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()

	m.SubmitProposal(&protocol.Proposal{SessionID: "s1", Approach: "a", Architecture: "b", Confidence: 80})
	ids := m.ActiveProposalIDs()

	err := m.SubmitVote(&protocol.Vote{
		SessionID:   "s1",
		RankedVotes: ids,
	})
	if err == nil {
		t.Error("expected error on self-vote")
	}
}

func TestContractRegistry(t *testing.T) {
	m := newTestMachine()
	m.RegisterContract(protocol.ContractEntry{
		SessionID:   "s1",
		ModuleName:  "predictor",
		ClassName:   "MarkovPredictor",
		Description: "Handles prediction logic",
	})
	entries := m.GetContracts()
	if len(entries) != 1 {
		t.Fatalf("contract entries = %d, want 1", len(entries))
	}
	if entries[0].ModuleName != "predictor" {
		t.Errorf("module = %q, want %q", entries[0].ModuleName, "predictor")
	}
}

func TestGetAllSessions(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.SessionRegistry.Register("s2", "simplicity")
	m.SessionRegistry.Register("s3", "performance")

	sessions := m.SessionRegistry.AllSessions()
	if len(sessions) != 3 {
		t.Errorf("sessions = %d, want 3", len(sessions))
	}
}

func TestActiveProposalIDs(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	m.RoundManager.Advance()

	m.SubmitProposal(&protocol.Proposal{SessionID: "s1", Approach: "a", Architecture: "b", Confidence: 80})
	ids := m.ActiveProposalIDs()
	if len(ids) != 1 {
		t.Errorf("active IDs = %d, want 1", len(ids))
	}
}

func TestSubmissionCount(t *testing.T) {
	m := newTestMachine()
	m.SessionRegistry.Register("s1", "correctness")
	if m.SubmissionCount() != 0 {
		t.Errorf("submissions = %d, want 0", m.SubmissionCount())
	}
	m.RoundManager.Advance()
	m.SubmitProposal(&protocol.Proposal{SessionID: "s1", Approach: "a", Architecture: "b", Confidence: 80})
	if m.SubmissionCount() != 1 {
		t.Errorf("submissions = %d, want 1", m.SubmissionCount())
	}
}

func newTestMachine() *Machine {
	taskBrief := &protocol.TaskBrief{
		TaskID:      "test-task",
		Description: "test",
		SwarmSize:   8,
	}
	timeouts := protocol.RoundTimeouts{
		Register: 30 * time.Second,
		Propose:  30 * time.Second,
		Critique: 45 * time.Second,
		Rebuttal: 30 * time.Second,
		Vote:     20 * time.Second,
	}
	return NewMachine("test-task", taskBrief, timeouts, timeouts.Register)
}
