package protocol

import (
	"fmt"
	"strings"
	"unicode"
)

var validResponses = map[string]bool{"agree": true, "concede": true, "defend": true}

// Max field lengths to prevent resource exhaustion and log injection.
const (
	MaxApproachLength     = 10000
	MaxArchitectureLength = 10000
	MaxRiskLength         = 2000
	MaxCritiqueLength     = 5000
	MaxFatalFlawLength    = 5000
	MaxRebuttalReasoning  = 5000
	MaxRisksCount         = 20
	MaxStrengthsCount     = 20
	MaxWeaknessesCount    = 20
	MaxRebuttalResponses  = 20
)

// ValidateProposal checks a Round 1 proposal.
func ValidateProposal(p *Proposal) error {
	if strings.TrimSpace(p.Approach) == "" {
		return ErrEmptyApproach
	}
	if len(p.Approach) > MaxApproachLength {
		return fmt.Errorf("approach exceeds %d characters", MaxApproachLength)
	}
	if len(p.Architecture) > MaxArchitectureLength {
		return fmt.Errorf("architecture exceeds %d characters", MaxArchitectureLength)
	}
	if len(p.Risks) > MaxRisksCount {
		return fmt.Errorf("risks list exceeds %d items", MaxRisksCount)
	}
	for i, risk := range p.Risks {
		if len(risk) > MaxRiskLength {
			return fmt.Errorf("risk[%d] exceeds %d characters", i, MaxRiskLength)
		}
	}
	if p.Confidence < 0 || p.Confidence > 100 {
		return ErrInvalidConfidence
	}
	if p.Subtasks < 0 {
		return fmt.Errorf("estimated_subtasks must be >= 0, got %d", p.Subtasks)
	}
	return nil
}

// ValidateCritique checks a Round 2 critique.
func ValidateCritique(c *Critique) error {
	if c.TargetProposalID == "" {
		return ErrNoTarget
	}
	if len(c.Strengths) == 0 && len(c.Weaknesses) == 0 {
		return ErrEmptyStrengths
	}
	if len(c.Strengths) > MaxStrengthsCount {
		return fmt.Errorf("strengths list exceeds %d items", MaxStrengthsCount)
	}
	if len(c.Weaknesses) > MaxWeaknessesCount {
		return fmt.Errorf("weaknesses list exceeds %d items", MaxWeaknessesCount)
	}
	for i, s := range c.Strengths {
		if len(s) > MaxCritiqueLength {
			return fmt.Errorf("strengths[%d] exceeds %d characters", i, MaxCritiqueLength)
		}
	}
	for i, w := range c.Weaknesses {
		if len(w) > MaxCritiqueLength {
			return fmt.Errorf("weaknesses[%d] exceeds %d characters", i, MaxCritiqueLength)
		}
	}
	if c.FatalFlaw != nil && len(*c.FatalFlaw) > MaxFatalFlawLength {
		return fmt.Errorf("fatal_flaw exceeds %d characters", MaxFatalFlawLength)
	}
	return nil
}

// ValidateRebuttal checks a Round 3 rebuttal.
func ValidateRebuttal(r *Rebuttal) error {
	if len(r.Responses) > MaxRebuttalResponses {
		return fmt.Errorf("rebuttal responses exceed %d items", MaxRebuttalResponses)
	}
	for i, resp := range r.Responses {
		if !validResponses[resp.Response] {
			return fmt.Errorf("rebuttal[%d]: %w: %q", i, ErrInvalidResponse, resp.Response)
		}
		if len(resp.Reasoning) > MaxRebuttalReasoning {
			return fmt.Errorf("rebuttal[%d] reasoning exceeds %d characters", i, MaxRebuttalReasoning)
		}
	}
	return nil
}

// ValidateVote checks a Round 4 vote.
func ValidateVote(v *Vote) error {
	if len(v.RankedVotes) == 0 {
		return ErrEmptyVote
	}
	if len(v.RankedVotes) > 50 {
		return fmt.Errorf("ranked_votes exceeds 50 items")
	}
	seen := make(map[string]bool)
	for _, pid := range v.RankedVotes {
		if len(pid) > 100 {
			return fmt.Errorf("proposal ID %q exceeds 100 characters", pid)
		}
		if seen[pid] {
			return fmt.Errorf("duplicate proposal %q in ranked votes", pid)
		}
		seen[pid] = true
	}
	return nil
}

// SanitizeLog strips control characters (except \n, \t) from a string
// to prevent ANSI escape injection and log forgery attacks.
func SanitizeLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1 // drop
		}
		return r
	}, s)
}
