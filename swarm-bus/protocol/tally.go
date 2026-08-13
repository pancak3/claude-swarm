package protocol

import (
	"errors"
	"sort"
)

// TallyVotes runs instant-runoff ranked-choice voting.
func TallyVotes(votes []Vote, activeProposals []string) (*TallyResult, error) {
	if len(activeProposals) == 0 {
		return nil, ErrNoProposals
	}
	if len(votes) == 0 {
		return nil, errors.New("no votes cast")
	}

	result := &TallyResult{
		RoundResults: make([]TallyRound, 0),
	}

	remaining := make(map[string]bool)
	for _, pid := range activeProposals {
		remaining[pid] = true
	}

	for len(remaining) > 1 {
		counts := make(map[string]int)
		for pid := range remaining {
			counts[pid] = 0
		}

		// Count continuing (non-exhausted) ballots only. Exhausted ballots
		// (voters whose entire ranking was eliminated) contribute no vote,
		// so the majority denominator must exclude them.
		continuingVotes := 0
		for _, vote := range votes {
			for _, pid := range vote.RankedVotes {
				if remaining[pid] {
					counts[pid]++
					continuingVotes++
					break
				}
			}
		}

		majority := continuingVotes/2 + 1
		tr := TallyRound{
			CandidateVotes: copyMap(counts),
			TotalVotes:     continuingVotes,
		}

		// Check for majority winner.
		for pid, count := range counts {
			if count >= majority {
				result.Winner = pid
				result.RoundResults = append(result.RoundResults, tr)
				return result, nil
			}
		}

		// Eliminate lowest — handle ties.
		lowestPID := ""
		lowestCount := continuingVotes + 1
		for _, count := range counts {
			if count < lowestCount {
				lowestCount = count
			}
		}

		// Pick lowest for elimination (deterministic by ID sort on tie).
		var tied []string
		for pid, count := range counts {
			if count == lowestCount {
				tied = append(tied, pid)
			}
		}
		sort.Strings(tied)
		lowestPID = tied[0]

		delete(remaining, lowestPID)
		result.Eliminated = append(result.Eliminated, lowestPID)
		tr.Eliminated = lowestPID
		result.RoundResults = append(result.RoundResults, tr)
	}

	// Winner. Normally a single candidate remains. If multiple remain
	// (all ballots exhausted with a zero-vote tie), break the tie
	// deterministically by sorted ID so the outcome is reproducible.
	remainingIDs := make([]string, 0, len(remaining))
	for pid := range remaining {
		remainingIDs = append(remainingIDs, pid)
	}
	sort.Strings(remainingIDs)
	if len(remainingIDs) > 0 {
		result.Winner = remainingIDs[0]
	}

	// Compute Gini from the first round's vote distribution (P3.4).
	if len(result.RoundResults) > 0 {
		result.DiversityScore = GiniCoefficient(result.RoundResults[0].CandidateVotes)
		if len(result.RoundResults[0].CandidateVotes) <= 1 {
			result.DegenerateVote = true
		}
	}

	return result, nil
}

// TallyVotesIncremental runs instant-runoff voting using voter preference tracking.
// Incrementally redistributes only eliminated candidates' voters per round,
// instead of scanning all votes — O(votes_of_eliminated) per round vs O(votes * remaining).
func TallyVotesIncremental(voterPrefs []VoterPref, activeProposals []string) (*TallyResult, error) {
	if len(activeProposals) == 0 {
		return nil, ErrNoProposals
	}
	if len(voterPrefs) == 0 {
		return nil, errors.New("no votes cast")
	}

	result := &TallyResult{
		RoundResults: make([]TallyRound, 0),
	}

	remaining := make(map[string]bool)
	for _, pid := range activeProposals {
		remaining[pid] = true
	}

	voterIndex := make([]int, len(voterPrefs))
	currentChoice := make([]string, len(voterPrefs))

	// Initialize: find each voter's first-ranked remaining candidate.
	for i, vp := range voterPrefs {
		voterIndex[i] = -1
		for j, pid := range vp.Ranked {
			if remaining[pid] {
				voterIndex[i] = j
				currentChoice[i] = pid
				break
			}
		}
	}

	for len(remaining) > 1 {
		counts := make(map[string]int)
		for pid := range remaining {
			counts[pid] = 0
		}
		// Count continuing (non-exhausted) ballots only. `currentChoice == ""`
		// marks an exhausted ballot, which must be excluded from the majority
		// denominator.
		continuingVotes := 0
		for _, pid := range currentChoice {
			if pid != "" {
				counts[pid]++
				continuingVotes++
			}
		}

		majority := continuingVotes/2 + 1
		tr := TallyRound{
			CandidateVotes: copyMap(counts),
			TotalVotes:     continuingVotes,
		}

		// Check for majority winner.
		for pid, count := range counts {
			if count >= majority {
				result.Winner = pid
				result.RoundResults = append(result.RoundResults, tr)
				return result, nil
			}
		}

		// Eliminate lowest (deterministic by ID sort on tie).
		lowestCount := continuingVotes + 1
		for _, count := range counts {
			if count < lowestCount {
				lowestCount = count
			}
		}
		var tied []string
		for pid, count := range counts {
			if count == lowestCount {
				tied = append(tied, pid)
			}
		}
		sort.Strings(tied)

		// If ALL remaining candidates have zero votes, ballots are fully exhausted.
		// Break early since eliminating from a zero-vote tie is misleading.
		if lowestCount == 0 {
			allZero := true
			for _, count := range counts {
				if count > 0 {
					allZero = false
					break
				}
			}
			if allZero {
				result.RoundResults = append(result.RoundResults, tr)
				break
			}
		}

		eliminatedPID := tied[0]

		delete(remaining, eliminatedPID)
		result.Eliminated = append(result.Eliminated, eliminatedPID)
		tr.Eliminated = eliminatedPID
		result.RoundResults = append(result.RoundResults, tr)

		// Redistribute only voters whose candidate was eliminated.
		for i, vp := range voterPrefs {
			if currentChoice[i] == eliminatedPID {
				found := false
				for j := voterIndex[i] + 1; j < len(vp.Ranked); j++ {
					if remaining[vp.Ranked[j]] {
						voterIndex[i] = j
						currentChoice[i] = vp.Ranked[j]
						found = true
						break
					}
				}
				if !found {
					currentChoice[i] = "" // exhausted ballot
				}
			}
		}
	}

	// Winner. Normally a single candidate remains. If multiple remain
	// (all ballots fully exhausted with a zero-vote tie), break the tie
	// deterministically by sorted ID so the outcome is reproducible.
	remainingIDs := make([]string, 0, len(remaining))
	for pid := range remaining {
		remainingIDs = append(remainingIDs, pid)
	}
	sort.Strings(remainingIDs)
	if len(remainingIDs) > 0 {
		result.Winner = remainingIDs[0]
	}

	// Compute Gini coefficient from final round's vote distribution (P3.4).
	if len(result.RoundResults) > 0 {
		result.DiversityScore = GiniCoefficient(result.RoundResults[0].CandidateVotes)
		if len(result.RoundResults[0].CandidateVotes) <= 1 {
			result.DegenerateVote = true
		}
	}

	return result, nil
}

// CountFatalFlaws returns proposals where >= threshold fraction of critiquing sessions flagged fatal_flaw.
// A single critiquing session cannot eliminate a proposal (denominator min 2).
func CountFatalFlaws(critiques []Critique, threshold float64) []string {
	counts := make(map[string]int)
	critiquingSessions := make(map[string]bool)
	for _, c := range critiques {
		critiquingSessions[c.SessionID] = true
		if c.FatalFlaw != nil && *c.FatalFlaw != "" {
			counts[c.TargetProposalID]++
		}
	}

	eliminated := make([]string, 0)
	totalCritiquing := len(critiquingSessions)
	if totalCritiquing < 2 {
		return eliminated
	}
	cutoff := int(float64(totalCritiquing) * threshold)
	if cutoff < 1 {
		cutoff = 1
	}
	for pid, count := range counts {
		if count >= cutoff {
			eliminated = append(eliminated, pid)
		}
	}
	return eliminated
}

// CountFatalFlawsFromIndex counts fatal flaws from a critique-index map.
// Uses per-proposal denominator: a proposal is eliminated when >= threshold
// fraction of sessions that critiqued THAT proposal flagged a fatal flaw.
// A single critiquing session cannot eliminate a proposal (denominator min 2).
func CountFatalFlawsFromIndex(index map[string][]*Critique, threshold float64) []string {
	counts := make(map[string]int)
	perProposalSessions := make(map[string]map[string]bool)

	for _, critiques := range index {
		for _, c := range critiques {
			if perProposalSessions[c.TargetProposalID] == nil {
				perProposalSessions[c.TargetProposalID] = make(map[string]bool)
			}
			perProposalSessions[c.TargetProposalID][c.SessionID] = true
			if c.FatalFlaw != nil && *c.FatalFlaw != "" {
				counts[c.TargetProposalID]++
			}
		}
	}

	eliminated := make([]string, 0)
	for pid, count := range counts {
		denominator := len(perProposalSessions[pid])
		if denominator < 2 {
			continue // single session cannot eliminate
		}
		cutoff := int(float64(denominator) * threshold)
		if cutoff < 1 {
			cutoff = 1
		}
		if count >= cutoff {
			eliminated = append(eliminated, pid)
		}
	}
	return eliminated
}

// RefutedFraction returns the fraction of a proposal's evidence claims that
// critiquing sessions marked "refuted". Returns 0 when no verdicts are present
// (e.g. sessions did not fill claim_verdicts, or the proposal listed no evidence).
func RefutedFraction(critiques []*Critique, proposalID string) float64 {
	total := 0
	refuted := 0
	for _, c := range critiques {
		if c == nil || c.TargetProposalID != proposalID {
			continue
		}
		for _, v := range c.ClaimVerdicts {
			total++
			if v == VerdictRefuted {
				refuted++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(refuted) / float64(total)
}

// GiniCoefficient computes the Gini coefficient of a vote distribution.
// Returns 0.0 (perfect equality) to 1.0 (perfect inequality).
// Uses the relative mean absolute difference formula.
func GiniCoefficient(voteCounts map[string]int) float64 {
	if len(voteCounts) == 0 {
		return 1.0
	}
	values := make([]int, 0, len(voteCounts))
	for _, v := range voteCounts {
		values = append(values, v)
	}
	sort.Ints(values)

	n := len(values)
	if n == 1 {
		return 0.0
	}

	// Gini = (2 * sum_i(i * y_i)) / (n * sum_i(y_i)) - (n+1)/n
	// where y_i are sorted values in ascending order (0-indexed).
	var sumValues int
	var weightedSum int
	for i, v := range values {
		sumValues += v
		weightedSum += (i + 1) * v
	}

	if sumValues == 0 {
		return 1.0
	}

	gini := float64(2*weightedSum)/float64(n*sumValues) - float64(n+1)/float64(n)
	if gini < 0 {
		gini = 0
	}
	return gini
}

func copyMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

