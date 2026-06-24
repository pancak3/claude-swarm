package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
)

// Lock ordering (must be consistent to prevent deadlocks):
// 1. proposalsMu  2. critiquesMu  3. votesMu
type Machine struct {
	proposalsMu         sync.RWMutex
	critiquesMu         sync.RWMutex
	votesMu             sync.RWMutex
	rebuttalsMu         sync.RWMutex
	contractsMu         sync.RWMutex
	voteResultMu        sync.RWMutex
	TaskID              string
	TaskBrief           *protocol.TaskBrief
	RoundManager        *RoundManager
	SubmitCh            chan struct{} // buffered(1) — signals round advancer of submissions
	SessionRegistry     *SessionRegistry
	proposals           map[string]*protocol.Proposal
	critiques           map[string]*protocol.Critique
	critiqueIndex       map[string][]*protocol.Critique
	rebuttals           map[string]*protocol.Rebuttal
	votes               map[string]*protocol.Vote
	voterPrefs          []protocol.VoterPref // incremental IRV
	eliminatedProposals map[string]bool
	contracts           []*protocol.ContractEntry
	voteResult          *protocol.TallyResult
}

// NewMachine creates a state machine for a task.
func NewMachine(taskID string, brief *protocol.TaskBrief, timeouts protocol.RoundTimeouts, registerTimeout time.Duration) *Machine {
	return &Machine{
		TaskID:              taskID,
		TaskBrief:           brief,
		RoundManager:        NewRoundManager(taskID, timeouts),
		SubmitCh:            make(chan struct{}, 1),
		SessionRegistry:     NewSessionRegistry(registerTimeout),
		proposals:           make(map[string]*protocol.Proposal),
		critiques:           make(map[string]*protocol.Critique),
		critiqueIndex:       make(map[string][]*protocol.Critique),
		rebuttals:           make(map[string]*protocol.Rebuttal),
		votes:               make(map[string]*protocol.Vote),
		voterPrefs:          make([]protocol.VoterPref, 0, 16),
		eliminatedProposals: make(map[string]bool),
		contracts:           make([]*protocol.ContractEntry, 0),
	}
}

// signalSubmit sends a non-blocking signal on the submit channel.
func (m *Machine) signalSubmit() {
	select {
	case m.SubmitCh <- struct{}{}:
	default:
	}
}

// SubmitProposal stores a proposal (only in PROPOSE round).
func (m *Machine) SubmitProposal(p *protocol.Proposal) error {
	m.proposalsMu.Lock()
	defer m.proposalsMu.Unlock()

	if m.RoundManager.Current() != protocol.RoundPropose {
		return fmt.Errorf("%w: current round is %s", protocol.ErrWrongRound, m.RoundManager.Current())
	}
	if _, exists := m.proposals[p.ID]; exists {
		return fmt.Errorf("proposal %q already submitted", p.ID)
	}
	m.proposals[p.ID] = p
	m.signalSubmit()
	return nil
}

// GetProposals returns all non-eliminated proposals.
func (m *Machine) GetProposals(anonymize bool, callerSessionID string) []*protocol.Proposal {
	m.proposalsMu.RLock()
	defer m.proposalsMu.RUnlock()

	result := make([]*protocol.Proposal, 0, len(m.proposals))
	for _, p := range m.proposals {
		if m.eliminatedProposals[p.ID] {
			continue
		}
		if anonymize && p.SessionID != callerSessionID {
			pc := *p
			pc.SessionID = ""
			result = append(result, &pc)
		} else {
			result = append(result, p)
		}
	}
	return result
}

// GetProposalByID returns a single proposal by ID (P3.4 self-vote check).
func (m *Machine) GetProposalByID(proposalID string) *protocol.Proposal {
	m.proposalsMu.RLock()
	defer m.proposalsMu.RUnlock()
	p, ok := m.proposals[proposalID]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// SubmitCritique stores a critique (only in CRITIQUE round).
func (m *Machine) SubmitCritique(c *protocol.Critique) error {
	m.proposalsMu.RLock()
	target, exists := m.proposals[c.TargetProposalID]
	m.proposalsMu.RUnlock()
	if !exists {
		return fmt.Errorf("%w: target proposal %q not found", protocol.ErrInvalidProposalRef, c.TargetProposalID)
	}
	if m.eliminatedProposals[c.TargetProposalID] {
		return fmt.Errorf("cannot critique eliminated proposal %q", c.TargetProposalID)
	}
	// A session cannot critique its own proposal.
	if target.SessionID == c.SessionID {
		return fmt.Errorf("self-critique not allowed: session %q cannot critique its own proposal", c.SessionID)
	}

	m.critiquesMu.Lock()
	defer m.critiquesMu.Unlock()

	if m.RoundManager.Current() != protocol.RoundCritique {
		return fmt.Errorf("%w: current round is %s", protocol.ErrWrongRound, m.RoundManager.Current())
	}
	if _, exists := m.critiques[c.ID]; exists {
		return fmt.Errorf("critique %q already submitted", c.ID)
	}
	// Prevent duplicate critiques from the same session against the same target.
	for _, existing := range m.critiques {
		if existing.SessionID == c.SessionID && existing.TargetProposalID == c.TargetProposalID {
			return fmt.Errorf("duplicate critique: session %q already critiqued proposal %q", c.SessionID, c.TargetProposalID)
		}
	}
	m.critiques[c.ID] = c
	m.critiqueIndex[c.TargetProposalID] = append(m.critiqueIndex[c.TargetProposalID], c)
	m.signalSubmit()
	return nil
}

// GetCritiquesForProposal returns critiques targeting a specific proposal (O(1) via index).
func (m *Machine) GetCritiquesForProposal(proposalID string) []*protocol.Critique {
	m.critiquesMu.RLock()
	defer m.critiquesMu.RUnlock()

	cs, ok := m.critiqueIndex[proposalID]
	if !ok {
		return nil
	}
	result := make([]*protocol.Critique, len(cs))
	copy(result, cs)
	return result
}

// EliminateByFatalFlaw checks all critiques and eliminates proposals with >= threshold fatal flaws.
func (m *Machine) EliminateByFatalFlaw(threshold float64) []string {
	m.critiquesMu.RLock()
	eliminated := protocol.CountFatalFlawsFromIndex(m.critiqueIndex, threshold)
	m.critiquesMu.RUnlock()

	m.proposalsMu.Lock()
	for _, pid := range eliminated {
		m.eliminatedProposals[pid] = true
	}
	m.proposalsMu.Unlock()
	return eliminated
}

// ActiveProposalIDs returns IDs of non-eliminated proposals.
func (m *Machine) ActiveProposalIDs() []string {
	m.proposalsMu.RLock()
	defer m.proposalsMu.RUnlock()

	ids := make([]string, 0, len(m.proposals))
	for id := range m.proposals {
		if !m.eliminatedProposals[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

// GetEliminatedProposals returns IDs of eliminated proposals.
func (m *Machine) GetEliminatedProposals() []string {
	m.proposalsMu.RLock()
	defer m.proposalsMu.RUnlock()

	ids := make([]string, 0, len(m.eliminatedProposals))
	for id := range m.eliminatedProposals {
		ids = append(ids, id)
	}
	return ids
}

// SubmitRebuttal stores a rebuttal (only in REBUTTAL round).
func (m *Machine) SubmitRebuttal(r *protocol.Rebuttal) error {
	m.rebuttalsMu.Lock()
	defer m.rebuttalsMu.Unlock()

	if m.RoundManager.Current() != protocol.RoundRebuttal {
		return fmt.Errorf("%w: current round is %s", protocol.ErrWrongRound, m.RoundManager.Current())
	}
	if _, exists := m.rebuttals[r.ID]; exists {
		return fmt.Errorf("rebuttal %q already submitted", r.ID)
	}
	m.rebuttals[r.ID] = r
	m.signalSubmit()
	return nil
}

// SubmitVote stores a vote (only in VOTE round).
func (m *Machine) SubmitVote(v *protocol.Vote) error {
	// Validate references before locking votesMu (lock ordering: proposalsMu then votesMu).
	m.proposalsMu.RLock()
	// Check self-voting: session cannot vote for its own proposal (P3.4).
	for _, pid := range v.RankedVotes {
		if prop, exists := m.proposals[pid]; exists {
			if prop.SessionID == v.SessionID {
				m.proposalsMu.RUnlock()
				return fmt.Errorf("self-voting not allowed: session %q cannot vote for its own proposal %q", v.SessionID, pid)
			}
		}
		if _, exists := m.proposals[pid]; !exists {
			m.proposalsMu.RUnlock()
			return fmt.Errorf("%w: proposal %q not found", protocol.ErrInvalidVoteRef, pid)
		}
		if m.eliminatedProposals[pid] {
			m.proposalsMu.RUnlock()
			return fmt.Errorf("%w: proposal %q has been eliminated", protocol.ErrInvalidVoteRef, pid)
		}
	}
	m.proposalsMu.RUnlock()

	m.votesMu.Lock()
	defer m.votesMu.Unlock()

	if m.RoundManager.Current() != protocol.RoundVote {
		return fmt.Errorf("%w: current round is %s", protocol.ErrWrongRound, m.RoundManager.Current())
	}
	if _, exists := m.votes[v.ID]; exists {
		return fmt.Errorf("vote %q already submitted", v.ID)
	}
	m.votes[v.ID] = v
	m.voterPrefs = append(m.voterPrefs, protocol.VoterPref{
		SessionID: v.SessionID,
		Ranked:    v.RankedVotes,
	})
	m.signalSubmit()
	return nil
}

// SnapshotVoteData returns votes and active proposals atomically under a single
// consistent lock acquisition order (proposalsMu then votesMu).
// Used by the round advancer to avoid TOCTOU between separate GetAllVotes
// and ActiveProposalIDs calls.
func (m *Machine) SnapshotVoteData() ([]protocol.Vote, []string) {
	m.proposalsMu.RLock()
	defer m.proposalsMu.RUnlock()
	m.votesMu.RLock()
	defer m.votesMu.RUnlock()

	votes := make([]protocol.Vote, 0, len(m.votes))
	for _, v := range m.votes {
		votes = append(votes, *v)
	}
	ids := make([]string, 0, len(m.proposals))
	for id := range m.proposals {
		if !m.eliminatedProposals[id] {
			ids = append(ids, id)
		}
	}
	return votes, ids
}

// GetAllVotes returns all cast votes.
func (m *Machine) GetAllVotes() []protocol.Vote {
	m.votesMu.RLock()
	defer m.votesMu.RUnlock()

	result := make([]protocol.Vote, 0, len(m.votes))
	for _, v := range m.votes {
		result = append(result, *v)
	}
	return result
}

// GetVoterPrefs returns voter preferences for incremental IRV tallying.
func (m *Machine) GetVoterPrefs() []protocol.VoterPref {
	m.votesMu.RLock()
	defer m.votesMu.RUnlock()
	cp := make([]protocol.VoterPref, len(m.voterPrefs))
	copy(cp, m.voterPrefs)
	return cp
}

// SetVoteResult stores the tally result from the VOTE round.
func (m *Machine) SetVoteResult(r *protocol.TallyResult) {
	m.voteResultMu.Lock()
	m.voteResult = r
	m.voteResultMu.Unlock()
}

// GetVoteResult returns the tally result from the VOTE round, or nil.
func (m *Machine) GetVoteResult() *protocol.TallyResult {
	m.voteResultMu.RLock()
	defer m.voteResultMu.RUnlock()
	return m.voteResult
}

// SubmissionCount returns how many sessions have submitted for the current round.
func (m *Machine) SubmissionCount() int {
	switch m.RoundManager.Current() {
	case protocol.RoundPropose:
		m.proposalsMu.RLock()
		defer m.proposalsMu.RUnlock()
		return len(m.proposals)
	case protocol.RoundCritique:
		m.critiquesMu.RLock()
		defer m.critiquesMu.RUnlock()
		return len(m.critiques)
	case protocol.RoundRebuttal:
		m.rebuttalsMu.RLock()
		defer m.rebuttalsMu.RUnlock()
		return len(m.rebuttals)
	case protocol.RoundVote:
		m.votesMu.RLock()
		defer m.votesMu.RUnlock()
		return len(m.votes)
	default:
		return 0
	}
}

// GetDeadlockRetries returns the deadlock retry count.
func (m *Machine) GetDeadlockRetries() int {
	return m.RoundManager.DeadlockCount()
}

// RegisterContract adds a contract entry (module/class name declaration).
func (m *Machine) RegisterContract(entry protocol.ContractEntry) {
	m.contractsMu.Lock()
	defer m.contractsMu.Unlock()
	m.contracts = append(m.contracts, &entry)
}

// GetContracts returns all registered contract entries.
func (m *Machine) GetContracts() []*protocol.ContractEntry {
	m.contractsMu.RLock()
	defer m.contractsMu.RUnlock()
	cp := make([]*protocol.ContractEntry, len(m.contracts))
	copy(cp, m.contracts)
	return cp
}

// GetContractEntries returns contract entries as a generic slice for HTTP serialization.
func (m *Machine) GetContractEntries() []map[string]string {
	m.contractsMu.RLock()
	defer m.contractsMu.RUnlock()
	result := make([]map[string]string, 0, len(m.contracts))
	for _, c := range m.contracts {
		result = append(result, map[string]string{
			"session_id":  c.SessionID,
			"module_name": c.ModuleName,
			"class_name":  c.ClassName,
			"description": c.Description,
		})
	}
	return result
}

// AddContractEntry creates and stores a contract entry from individual fields.
func (m *Machine) AddContractEntry(sessionID, moduleName, className, description string) {
	m.contractsMu.Lock()
	defer m.contractsMu.Unlock()
	m.contracts = append(m.contracts, &protocol.ContractEntry{
		SessionID:   sessionID,
		ModuleName:  moduleName,
		ClassName:   className,
		Description: description,
	})
}

// StatusSnapshot returns a JSON-serializable snapshot of the machine state.
type StatusSnapshot struct {
	TaskID             string                 `json:"task_id"`
	Round              protocol.Round         `json:"round"`
	TimeRemaining      string                 `json:"time_remaining"`
	ActiveSessions     int                    `json:"active_sessions"`
	TotalSessions      int                    `json:"total_sessions"`
	ProposalsSubmitted int                    `json:"proposals_submitted"`
	CritiquesSubmitted int                    `json:"critiques_submitted"`
	RebuttalsSubmitted int                    `json:"rebuttals_submitted"`
	VotesCast          int                    `json:"votes_cast"`
	Sessions           []protocol.SessionInfo `json:"sessions"`
	Winner             string                 `json:"winner,omitempty"`
	VoteRounds         []protocol.TallyRound  `json:"vote_rounds,omitempty"`
	Proposals          []proposalSnapshot     `json:"proposals,omitempty"`
	DiversityScore     *float64               `json:"diversity_score,omitempty"` // Gini coefficient (P3.4)
	DegenerateVote     bool                   `json:"degenerate_vote,omitempty"` // true when <=1 candidate in first round
}

// proposalSnapshot is a public-safe proposal view for the /status endpoint.
type proposalSnapshot struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	Perspective  string   `json:"perspective"`
	Approach     string   `json:"approach"`
	Architecture string   `json:"architecture"`
	Risks        []string `json:"risks"`
	Subtasks     int      `json:"estimated_subtasks"`
	Confidence   int      `json:"confidence"`
	Eliminated   bool     `json:"eliminated"`
}

func (m *Machine) StatusSnapshot() *StatusSnapshot {
	// Use SwarmSize as the denominator (expected total), not hardcoded 3.
	expectedTotal := m.TaskBrief.SwarmSize
	allCount := len(m.SessionRegistry.AllSessions())
	if allCount > expectedTotal {
		expectedTotal = allCount // more registered than expected — show reality
	}
	snap := &StatusSnapshot{
		TaskID:         m.TaskID,
		Round:          m.RoundManager.Current(),
		TimeRemaining:  m.RoundManager.TimeRemaining().Round(time.Second).String(),
		ActiveSessions: m.SessionRegistry.ActiveCount(),
		TotalSessions:  expectedTotal,
	}

	m.proposalsMu.RLock()
	snap.ProposalsSubmitted = len(m.proposals)
	m.proposalsMu.RUnlock()

	m.critiquesMu.RLock()
	snap.CritiquesSubmitted = len(m.critiques)
	m.critiquesMu.RUnlock()

	m.rebuttalsMu.RLock()
	snap.RebuttalsSubmitted = len(m.rebuttals)
	m.rebuttalsMu.RUnlock()

	m.votesMu.RLock()
	snap.VotesCast = len(m.votes)
	m.votesMu.RUnlock()

	// Include vote result if available.
	m.voteResultMu.RLock()
	if m.voteResult != nil {
		snap.Winner = m.voteResult.Winner
		snap.VoteRounds = m.voteResult.RoundResults
		snap.DiversityScore = &m.voteResult.DiversityScore
			snap.DegenerateVote = m.voteResult.DegenerateVote
	}
	m.voteResultMu.RUnlock()

	// Load sessions before proposals so perspective matching works.
	snap.Sessions = m.SessionRegistry.AllSessions()

	// Include proposals with session perspectives.
	m.proposalsMu.RLock()
	snap.Proposals = make([]proposalSnapshot, 0, len(m.proposals))
	for _, p := range m.proposals {
		ps := proposalSnapshot{
			ID:           p.ID,
			SessionID:    p.SessionID,
			Approach:     p.Approach,
			Architecture: p.Architecture,
			Risks:        p.Risks,
			Subtasks:     p.Subtasks,
			Confidence:   p.Confidence,
			Eliminated:   m.eliminatedProposals[p.ID],
		}
		for _, si := range snap.Sessions {
			if si.ID == p.SessionID {
				ps.Perspective = si.Perspective
				break
			}
		}
		snap.Proposals = append(snap.Proposals, ps)
	}
	m.proposalsMu.RUnlock()

	return snap
}

// CheckpointData holds serializable swarm state for checkpoint/resume (P2.1).
type CheckpointData struct {
	TaskID              string                         `json:"task_id"`
	Round               protocol.Round                 `json:"round"`
	RoundStartUnix      int64                          `json:"round_start_unix"`
	Proposals           map[string]*protocol.Proposal  `json:"proposals,omitempty"`
	Critiques           map[string]*protocol.Critique  `json:"critiques,omitempty"`
	CritiqueIndex       map[string][]string            `json:"critique_index,omitempty"`
	Rebuttals           map[string]*protocol.Rebuttal  `json:"rebuttals,omitempty"`
	Votes               map[string]*protocol.Vote      `json:"votes,omitempty"`
	VoterPrefs          []protocol.VoterPref           `json:"voter_prefs,omitempty"`
	EliminatedProposals map[string]bool                `json:"eliminated_proposals,omitempty"`
	Contracts           []*protocol.ContractEntry      `json:"contracts,omitempty"`
	VoteResult          *protocol.TallyResult          `json:"vote_result,omitempty"`
	Sessions            []protocol.SessionInfo         `json:"sessions,omitempty"`
	AuthTokens          map[string]string              `json:"auth_tokens,omitempty"`
	DeadlockCount       int                            `json:"deadlock_count"`
	FastPath            bool                           `json:"fast_path"`
}

// SaveCheckpoint writes current swarm state to a JSON checkpoint file (P2.1).
func (m *Machine) SaveCheckpoint(path string) error {
	m.proposalsMu.RLock()
	m.critiquesMu.RLock()
	m.rebuttalsMu.RLock()
	m.votesMu.RLock()
	m.voteResultMu.RLock()
	m.contractsMu.RLock()

	ci := make(map[string][]string)
	for pid, cs := range m.critiqueIndex {
		ids := make([]string, len(cs))
		for i, c := range cs {
			ids[i] = c.ID
		}
		ci[pid] = ids
	}

	cp := &CheckpointData{
		TaskID:              m.TaskID,
		Round:               m.RoundManager.Current(),
		RoundStartUnix:      time.Now().Unix(),
		Proposals:           m.proposals,
		Critiques:           m.critiques,
		CritiqueIndex:       ci,
		Rebuttals:           m.rebuttals,
		Votes:               m.votes,
		VoterPrefs:          m.voterPrefs,
		EliminatedProposals: m.eliminatedProposals,
		Contracts:           m.contracts,
		VoteResult:          m.voteResult,
		DeadlockCount:       m.RoundManager.DeadlockCount(),
		FastPath:            m.RoundManager.FastPath(),
	}

	m.contractsMu.RUnlock()
	m.voteResultMu.RUnlock()
	m.votesMu.RUnlock()
	m.rebuttalsMu.RUnlock()
	m.critiquesMu.RUnlock()
	m.proposalsMu.RUnlock()

	cp.Sessions = m.SessionRegistry.AllSessions()
	cp.AuthTokens = m.SessionRegistry.AllAuthTokens()

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("checkpoint mkdir: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("checkpoint write: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("checkpoint rename: %w", err)
	}
	return nil
}

// LoadCheckpoint restores machine state from a JSON checkpoint file (P2.1).
func (m *Machine) LoadCheckpoint(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cp CheckpointData
	if err := json.Unmarshal(data, &cp); err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint parse error: %v\n", err)
		return false
	}

	m.proposalsMu.Lock()
	m.critiquesMu.Lock()
	m.rebuttalsMu.Lock()
	m.votesMu.Lock()
	m.voteResultMu.Lock()
	m.contractsMu.Lock()

	m.TaskID = cp.TaskID
	if cp.Proposals != nil {
		m.proposals = cp.Proposals
	}
	if cp.Critiques != nil {
		m.critiques = cp.Critiques
	}
	if cp.Rebuttals != nil {
		m.rebuttals = cp.Rebuttals
	}
	if cp.Votes != nil {
		m.votes = cp.Votes
	}
	if cp.VoterPrefs != nil {
		m.voterPrefs = cp.VoterPrefs
	}
	if cp.EliminatedProposals != nil {
		m.eliminatedProposals = cp.EliminatedProposals
	}
	if cp.Contracts != nil {
		m.contracts = cp.Contracts
	}
	if cp.VoteResult != nil {
		m.voteResult = cp.VoteResult
	}

	// Rebuild critique index.
	if cp.CritiqueIndex != nil {
		m.critiqueIndex = make(map[string][]*protocol.Critique)
		for pid, ids := range cp.CritiqueIndex {
			for _, cid := range ids {
				if c, ok := m.critiques[cid]; ok {
					m.critiqueIndex[pid] = append(m.critiqueIndex[pid], c)
				}
			}
		}
	} else {
		m.critiqueIndex = make(map[string][]*protocol.Critique)
		for _, c := range m.critiques {
			m.critiqueIndex[c.TargetProposalID] = append(m.critiqueIndex[c.TargetProposalID], c)
		}
	}

	m.contractsMu.Unlock()
	m.voteResultMu.Unlock()
	m.votesMu.Unlock()
	m.rebuttalsMu.Unlock()
	m.critiquesMu.Unlock()
	m.proposalsMu.Unlock()

	if cp.Sessions != nil {
		for _, si := range cp.Sessions {
			m.SessionRegistry.RestoreSession(si)
		}
	}
	for sid, token := range cp.AuthTokens {
		m.SessionRegistry.RestoreAuthToken(sid, token)
	}

	m.RoundManager.ForceRound(cp.Round)

	fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint restored: round=%s proposals=%d critiques=%d votes=%d sessions=%d\n",
		cp.Round, len(cp.Proposals), len(cp.Critiques), len(cp.Votes), len(cp.Sessions))
	return true
}
