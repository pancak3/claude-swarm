package bus_test

import (
	"testing"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
)

func TestTallySimpleMajority(t *testing.T) {
	votes := []protocol.Vote{
		{ID: "v1", SessionID: "s1", RankedVotes: []string{"p1", "p2"}},
		{ID: "v2", SessionID: "s2", RankedVotes: []string{"p1", "p2"}},
		{ID: "v3", SessionID: "s3", RankedVotes: []string{"p2", "p1"}},
	}

	result, err := protocol.TallyVotes(votes, []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p1" {
		t.Errorf("expected p1 winner with 2/3 votes, got %s", result.Winner)
	}
}

func TestTallyInstantRunoff(t *testing.T) {
	votes := []protocol.Vote{
		{ID: "v1", SessionID: "s1", RankedVotes: []string{"p1", "p2", "p3"}},
		{ID: "v2", SessionID: "s2", RankedVotes: []string{"p1", "p3", "p2"}},
		{ID: "v3", SessionID: "s3", RankedVotes: []string{"p2", "p3", "p1"}},
		{ID: "v4", SessionID: "s4", RankedVotes: []string{"p2", "p3", "p1"}},
		{ID: "v5", SessionID: "s5", RankedVotes: []string{"p3", "p1", "p2"}},
	}

	result, err := protocol.TallyVotes(votes, []string{"p1", "p2", "p3"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p1" {
		t.Errorf("expected p1 winner after runoff, got %s", result.Winner)
	}
}

func TestTallySimpleRunoffNoDeadlock(t *testing.T) {
	votes := []protocol.Vote{
		{ID: "v1", SessionID: "s1", RankedVotes: []string{"p1", "p2"}},
		{ID: "v2", SessionID: "s2", RankedVotes: []string{"p2", "p1"}},
	}

	result, err := protocol.TallyVotes(votes, []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p1" {
		t.Logf("tie broken: %s wins", result.Winner)
	}
}

func TestTallyIncrementalSimpleMajority(t *testing.T) {
	prefs := []protocol.VoterPref{
		{SessionID: "s1", Ranked: []string{"p1", "p2"}},
		{SessionID: "s2", Ranked: []string{"p1", "p2"}},
		{SessionID: "s3", Ranked: []string{"p2", "p1"}},
	}

	result, err := protocol.TallyVotesIncremental(prefs, []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p1" {
		t.Errorf("expected p1 winner, got %s", result.Winner)
	}
}

func TestTallyIncrementalRunoff(t *testing.T) {
	prefs := []protocol.VoterPref{
		{SessionID: "s1", Ranked: []string{"p1", "p2", "p3"}},
		{SessionID: "s2", Ranked: []string{"p1", "p3", "p2"}},
		{SessionID: "s3", Ranked: []string{"p2", "p3", "p1"}},
		{SessionID: "s4", Ranked: []string{"p2", "p3", "p1"}},
		{SessionID: "s5", Ranked: []string{"p3", "p1", "p2"}},
	}

	result, err := protocol.TallyVotesIncremental(prefs, []string{"p1", "p2", "p3"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p1" {
		t.Errorf("expected p1 winner after runoff, got %s", result.Winner)
	}
}

func TestTallyIncrementalExhaustedBallots(t *testing.T) {
	// All voters exhaust their ballots — no active candidate gets votes.
	prefs := []protocol.VoterPref{
		{SessionID: "s1", Ranked: []string{"p3", "p1", "p2"}},
		{SessionID: "s2", Ranked: []string{"p3", "p2", "p1"}},
	}
	result, err := protocol.TallyVotesIncremental(prefs, []string{"p1", "p2", "p3"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p3" {
		t.Errorf("expected p3 winner (first preference), got %s", result.Winner)
	}
}

func TestTallyIncrementalPartialExhaustion(t *testing.T) {
	// One voter exhausts, the other doesn't.
	prefs := []protocol.VoterPref{
		{SessionID: "s1", Ranked: []string{"p3", "p1"}},
		{SessionID: "s2", Ranked: []string{"p2", "p3"}},
	}
	// p1 eliminated first (0 votes), then s2's first choice p2 gets eliminated,
	// s2's vote redistributes to p3 → p3 wins 2-0.
	result, err := protocol.TallyVotesIncremental(prefs, []string{"p1", "p2", "p3"})
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}
	if result.Winner != "p3" {
		t.Errorf("expected p3 winner, got %s", result.Winner)
	}
}

func TestCountFatalFlaws(t *testing.T) {
	ff := "fatal"
	critiques := []protocol.Critique{
		{ID: "c1", SessionID: "s1", TargetProposalID: "p1", FatalFlaw: &ff},
		{ID: "c2", SessionID: "s2", TargetProposalID: "p1", FatalFlaw: &ff},
		{ID: "c3", SessionID: "s3", TargetProposalID: "p2"},
	}

	eliminated := protocol.CountFatalFlaws(critiques, 0.5)
	if len(eliminated) != 1 || eliminated[0] != "p1" {
		t.Errorf("expected p1 eliminated, got %v", eliminated)
	}
}

func TestValidateProposal(t *testing.T) {
	tests := []struct {
		name    string
		p       protocol.Proposal
		wantErr bool
	}{
		{"valid", protocol.Proposal{Approach: "good", Confidence: 80, Subtasks: 3}, false},
		{"empty approach", protocol.Proposal{Approach: "", Confidence: 80}, true},
		{"negative confidence", protocol.Proposal{Approach: "x", Confidence: -1}, true},
		{"confidence over 100", protocol.Proposal{Approach: "x", Confidence: 101}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := protocol.ValidateProposal(&tt.p)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProposal() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
