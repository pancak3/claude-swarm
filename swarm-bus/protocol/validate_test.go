package protocol

import (
	"strings"
	"testing"
)

// =============================================================================
// ValidateCritique tests (currently 0% coverage)
// =============================================================================

func TestValidateCritique(t *testing.T) {
	tests := []struct {
		name    string
		c       Critique
		wantErr bool
	}{
		{"valid full", Critique{TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{"bad"}}, false},
		{"valid strengths only", Critique{TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{}}, false},
		{"valid weaknesses only", Critique{TargetProposalID: "p1", Strengths: []string{}, Weaknesses: []string{"bad"}}, false},
		{"empty target", Critique{TargetProposalID: "", Strengths: []string{"good"}}, true},
		{"empty both strengths and weaknesses", Critique{TargetProposalID: "p1", Strengths: []string{}, Weaknesses: []string{}}, true},
		{"strengths exceed max", Critique{TargetProposalID: "p1", Strengths: makeOverMaxCritique(), Weaknesses: []string{"x"}}, true},
		{"weaknesses exceed max", Critique{TargetProposalID: "p1", Strengths: []string{"x"}, Weaknesses: makeOverMaxCritique()}, true},
		{"strength too long", Critique{TargetProposalID: "p1", Strengths: []string{makeLongString(MaxCritiqueLength + 1)}, Weaknesses: []string{}}, true},
		{"weakness too long", Critique{TargetProposalID: "p1", Strengths: []string{}, Weaknesses: []string{makeLongString(MaxCritiqueLength + 1)}}, true},
		{"fatal flaw too long", Critique{TargetProposalID: "p1", Strengths: []string{"x"}, Weaknesses: []string{}, FatalFlaw: strPtr(makeLongString(MaxFatalFlawLength + 1))}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCritique(&tt.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCritique() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCritiqueFatalFlawNilAllowed(t *testing.T) {
	c := &Critique{TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{"bad"}, FatalFlaw: nil}
	if err := ValidateCritique(c); err != nil {
		t.Errorf("nil fatal_flaw should be valid: %v", err)
	}
}

func TestValidateCritiqueFatalFlawEmptyAllowed(t *testing.T) {
	ff := ""
	c := &Critique{TargetProposalID: "p1", Strengths: []string{"good"}, Weaknesses: []string{}, FatalFlaw: &ff}
	if err := ValidateCritique(c); err != nil {
		t.Errorf("empty fatal_flaw should be valid: %v", err)
	}
}

// =============================================================================
// ValidateRebuttal additional tests
// =============================================================================

func TestValidateRebuttalFull(t *testing.T) {
	tests := []struct {
		name    string
		r       Rebuttal
		wantErr bool
	}{
		{"valid defend", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "slow", Response: "defend", Reasoning: "correctness needs checks"}}}, false},
		{"valid agree", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "fragile", Response: "agree", AmendedApproach: "add tests"}}}, false},
		{"valid concede", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "complex", Response: "concede", AmendedApproach: "simplify"}}}, false},
		{"invalid response", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "x", Response: "reject"}}}, true},
		{"empty response", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "x", Response: ""}}}, true},
		{"too many responses", Rebuttal{Responses: makeOverMaxResponses()}, true},
		{"reasoning too long", Rebuttal{Responses: []RebuttalResponse{{CritiquePoint: "x", Response: "defend", Reasoning: makeLongString(MaxRebuttalReasoning + 1)}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRebuttal(&tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRebuttal() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ValidateVote additional tests
// =============================================================================

func TestValidateVoteFull(t *testing.T) {
	tests := []struct {
		name    string
		v       Vote
		wantErr bool
	}{
		{"valid", Vote{RankedVotes: []string{"p1", "p2"}}, false},
		{"empty", Vote{RankedVotes: []string{}}, true},
		{"duplicate", Vote{RankedVotes: []string{"p1", "p2", "p1"}}, true},
		{"too many", Vote{RankedVotes: makeOverMaxVotes()}, true},
		{"long ID", Vote{RankedVotes: []string{makeLongString(101)}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVote(&tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVote() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ValidateProposal additional tests (length limits)
// =============================================================================

func TestValidateProposalLengthLimits(t *testing.T) {
	tests := []struct {
		name    string
		p       Proposal
		wantErr bool
	}{
		{"approach too long", Proposal{Approach: makeLongString(MaxApproachLength + 1), Confidence: 50, Subtasks: 1}, true},
		{"architecture too long", Proposal{Approach: "ok", Architecture: makeLongString(MaxArchitectureLength + 1), Confidence: 50, Subtasks: 1}, true},
		{"too many risks", Proposal{Approach: "ok", Risks: makeOverMaxRisks(), Confidence: 50, Subtasks: 1}, true},
		{"risk item too long", Proposal{Approach: "ok", Risks: []string{makeLongString(MaxRiskLength + 1)}, Confidence: 50, Subtasks: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProposal(&tt.p)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProposal() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// SanitizeLog tests (currently 0% coverage)
// =============================================================================

func TestSanitizeLogClean(t *testing.T) {
	input := "Hello, world!"
	output := SanitizeLog(input)
	if output != input {
		t.Errorf("clean input should be unchanged: got %q", output)
	}
}

func TestSanitizeLogWithNewlines(t *testing.T) {
	input := "line1\nline2\nline3"
	output := SanitizeLog(input)
	if output != input {
		t.Errorf("newlines should be preserved: got %q", output)
	}
}

func TestSanitizeLogWithTabs(t *testing.T) {
	input := "col1\tcol2\tcol3"
	output := SanitizeLog(input)
	if output != input {
		t.Errorf("tabs should be preserved: got %q", output)
	}
}

func TestSanitizeLogStripsControlChars(t *testing.T) {
	input := "hello\x00world\x01test\x1b[31mred\x1b[0m"
	output := SanitizeLog(input)
	if strings.Contains(output, "\x00") || strings.Contains(output, "\x1b") {
		t.Errorf("control characters should be stripped: got %q", output)
	}
	if !strings.Contains(output, "helloworld") {
		t.Errorf("non-control chars should be preserved: got %q", output)
	}
}

func TestSanitizeLogEmpty(t *testing.T) {
	output := SanitizeLog("")
	if output != "" {
		t.Errorf("empty input should return empty: got %q", output)
	}
}

func TestSanitizeLogOnlyControlChars(t *testing.T) {
	input := "\x00\x01\x02\x03"
	output := SanitizeLog(input)
	if output != "" {
		t.Errorf("all-control-char input should return empty: got %q", output)
	}
}

func TestSanitizeLogMixed(t *testing.T) {
	input := "normal\x00text\x01with\x1bcontrol\nand\ttabs"
	output := SanitizeLog(input)
	// \x00, \x01, \x1b are stripped. \n and \t are preserved.
	expected := "normaltextwithcontrol\nand\ttabs"
	if output != expected {
		t.Errorf("unexpected output: got %q want %q", output, expected)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func makeLongString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func strPtr(s string) *string {
	return &s
}

func makeOverMaxCritique() []string {
	result := make([]string, MaxStrengthsCount+1)
	for i := range result {
		result[i] = "item"
	}
	return result
}

func makeOverMaxResponses() []RebuttalResponse {
	result := make([]RebuttalResponse, MaxRebuttalResponses+1)
	for i := range result {
		result[i] = RebuttalResponse{
			CritiquePoint: "point",
			Response:      "defend",
		}
	}
	return result
}

func makeOverMaxVotes() []string {
	result := make([]string, 51)
	for i := range result {
		result[i] = "p1"
	}
	return result
}

func makeOverMaxRisks() []string {
	result := make([]string, MaxRisksCount+1)
	for i := range result {
		result[i] = "risk"
	}
	return result
}
