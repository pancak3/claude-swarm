package protocol

import (
	"errors"
	"time"
)

// Round represents a parliamentary round state.
type Round string

const (
	RoundRegistering Round = "REGISTERING"
	RoundPropose     Round = "PROPOSE"
	RoundCritique    Round = "CRITIQUE"
	RoundRebuttal    Round = "REBUTTAL"
	RoundVote        Round = "VOTE"
	RoundExecute     Round = "EXECUTE"
	RoundSynthesis   Round = "SYNTHESIS"
	RoundClosed      Round = "CLOSED"
	RoundDeadlock    Round = "DEADLOCK"
)

var (
	ErrWrongRound       = errors.New("submission not allowed in current round")
	ErrEmptyApproach    = errors.New("proposal approach must not be empty")
	ErrInvalidConfidence = errors.New("confidence must be 0-100")
	ErrNoTarget         = errors.New("critique must specify target_proposal_id")
	ErrEmptyStrengths   = errors.New("critique must have at least one strength or weakness")
	ErrInvalidResponse  = errors.New("rebuttal response must be agree, concede, or defend")
	ErrEmptyVote        = errors.New("vote must include at least one ranked choice")
	ErrNoProposals      = errors.New("no proposals to tally")
	ErrInvalidProposalRef = errors.New("critique references non-existent proposal")
	ErrInvalidVoteRef   = errors.New("vote references non-existent or eliminated proposal")
)

// RoundTimeouts holds per-round timeout configuration.
type RoundTimeouts struct {
	Register time.Duration `json:"register"`
	Propose  time.Duration `json:"propose"`
	Critique time.Duration `json:"critique"`
	Rebuttal time.Duration `json:"rebuttal"`
	Vote     time.Duration `json:"vote"`
}

// Proposal is a Round 1 submission from a session.
type Proposal struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	Approach     string    `json:"approach"`
	Architecture string    `json:"architecture"`
	Risks        []string  `json:"risks"`
	Subtasks     int       `json:"estimated_subtasks"`
	Confidence   int       `json:"confidence"`
	Timestamp    time.Time `json:"timestamp"`
}

// Critique is a Round 2 submission targeting a specific proposal.
type Critique struct {
	ID               string  `json:"id"`
	SessionID        string  `json:"session_id"`
	TargetProposalID string  `json:"target_proposal_id"`
	Strengths        []string `json:"strengths"`
	Weaknesses       []string `json:"weaknesses"`
	FatalFlaw        *string `json:"fatal_flaw,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}

// RebuttalResponse is a single response within a Rebuttal submission.
type RebuttalResponse struct {
	CritiquePoint   string `json:"critique_point"`
	Response        string `json:"response"`
	AmendedApproach string `json:"amended_approach,omitempty"`
	Reasoning       string `json:"reasoning,omitempty"`
}

// Rebuttal is a Round 3 submission from a session defending its proposal.
type Rebuttal struct {
	ID        string             `json:"id"`
	SessionID string             `json:"session_id"`
	Responses []RebuttalResponse `json:"rebuttals"`
	Timestamp time.Time          `json:"timestamp"`
}

// Vote is a Round 4 ranked-choice vote from a session.
type Vote struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	RankedVotes []string  `json:"ranked_votes"`
	Timestamp   time.Time `json:"timestamp"`
}

// VoterPref stores a parsed preference ballot for incremental IRV tallying.
type VoterPref struct {
	SessionID string   `json:"session_id"`
	Ranked    []string `json:"ranked"`
}

// SessionInfo tracks a connected session.
type SessionInfo struct {
	ID           string    `json:"id"`
	RegisteredAt time.Time `json:"registered_at"`
	LastSeen     time.Time `json:"last_seen"`
	Active       bool      `json:"active"`
	Perspective  string    `json:"perspective"`
	TokensIn     int64     `json:"tokens_in"`
	TokensOut    int64     `json:"tokens_out"`
}

// RoundResult is the summary returned after each round.
type RoundResult struct {
	Round               Round         `json:"round"`
	ProposalsSubmitted  int           `json:"proposals_submitted"`
	ProposalsEliminated int           `json:"proposals_eliminated,omitempty"`
	VoteTally           map[string]int `json:"vote_tally,omitempty"`
	Winner              string        `json:"winner,omitempty"`
	NextRound           Round         `json:"next_round"`
	SessionsActive      int           `json:"sessions_active"`
	Summary             string        `json:"summary"`
}

// TaskBrief is the task description delivered to a session on register.
type TaskBrief struct {
	TaskID         string `json:"task_id"`
	Description    string `json:"description"`
	SwarmSize      int    `json:"swarm_size"`
	OrchestratorID string `json:"orchestrator_id"`
	Depth          int    `json:"depth"`
	MaxDepth       int    `json:"max_depth"`
}

// ContractEntry is a single module/class name registration shared across sessions.
// Sessions declare names here before coding to prevent divergent naming.
type ContractEntry struct {
	SessionID   string `json:"session_id"`
	ModuleName  string `json:"module_name"`
	ClassName   string `json:"class_name"`
	Description string `json:"description,omitempty"`
}

// TallyResult holds the outcome of a ranked-choice vote.
type TallyResult struct {
	Winner          string       `json:"winner,omitempty"`
	RoundResults    []TallyRound `json:"round_results"`
	Eliminated      []string     `json:"eliminated"`
	DiversityScore  float64      `json:"diversity_score,omitempty"` // Gini coefficient (P3.4)
	DegenerateVote  bool         `json:"degenerate_vote,omitempty"` // true when <=1 candidate in first round
}

// TallyRound is one round of instant-runoff counting.
type TallyRound struct {
	CandidateVotes map[string]int `json:"candidate_votes"`
	Eliminated     string         `json:"eliminated,omitempty"`
	TotalVotes     int            `json:"total_votes"`
}

