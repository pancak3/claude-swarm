package state

import (
	"sync"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
)

// RoundManager handles round transitions and timeouts.
type RoundManager struct {
	mu            sync.RWMutex
	currentRound  protocol.Round
	roundStart    time.Time
	timeouts      protocol.RoundTimeouts
	deadlockCount int
	taskID        string
	fastPath      bool // skip CRITIQUE and REBUTTAL rounds
}

// NewRoundManager creates a round manager for a task.
func NewRoundManager(taskID string, timeouts protocol.RoundTimeouts) *RoundManager {
	return &RoundManager{
		currentRound: protocol.RoundRegistering,
		roundStart:   time.Now(),
		timeouts:     timeouts,
		taskID:       taskID,
	}
}

// SetFastPath enables 3-round mode (skip CRITIQUE and REBUTTAL).
func (rm *RoundManager) SetFastPath(enabled bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.fastPath = enabled
}

// FastPath returns whether fast-path is enabled.
func (rm *RoundManager) FastPath() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.fastPath
}

// Current returns the current round.
func (rm *RoundManager) Current() protocol.Round {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.currentRound
}

// TimeoutFor returns the timeout for the given round.
func (rm *RoundManager) TimeoutFor(round protocol.Round) time.Duration {
	switch round {
	case protocol.RoundRegistering:
		return rm.timeouts.Register
	case protocol.RoundPropose:
		return rm.timeouts.Propose
	case protocol.RoundCritique:
		return rm.timeouts.Critique
	case protocol.RoundRebuttal:
		return rm.timeouts.Rebuttal
	case protocol.RoundVote:
		return rm.timeouts.Vote
	default:
		return 30 * time.Second
	}
}

// TimeRemaining returns how much time is left in the current round.
func (rm *RoundManager) TimeRemaining() time.Duration {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	elapsed := time.Since(rm.roundStart)
	timeout := rm.TimeoutFor(rm.currentRound)
	if elapsed >= timeout {
		return 0
	}
	return timeout - elapsed
}

// IsExpired checks if the current round has timed out.
func (rm *RoundManager) IsExpired() bool {
	return rm.TimeRemaining() <= 0
}

// Advance moves to the next round and resets the deadlock counter.
func (rm *RoundManager) Advance() protocol.Round {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.deadlockCount = 0 // reset per-phase retry counter
	next := rm.nextRound(rm.currentRound)
	rm.currentRound = next
	rm.roundStart = time.Now()
	return next
}

// ForceRound sets the current round to an arbitrary value (used for aborting on fatal errors).
func (rm *RoundManager) ForceRound(round protocol.Round) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.currentRound = round
	rm.roundStart = time.Now()
}

// IncrementDeadlockCount increments the deadlock retry counter.
func (rm *RoundManager) IncrementDeadlockCount() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.deadlockCount++
}

// DeadlockCount returns the current deadlock retry count.
func (rm *RoundManager) DeadlockCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.deadlockCount
}

func (rm *RoundManager) nextRound(current protocol.Round) protocol.Round {
	// Fast path: skip CRITIQUE and REBUTTAL — PROPOSE → VOTE.
	if rm.fastPath {
		switch current {
		case protocol.RoundRegistering:
			return protocol.RoundPropose
		case protocol.RoundPropose:
			return protocol.RoundVote
		case protocol.RoundVote:
			return protocol.RoundExecute
		case protocol.RoundExecute:
			return protocol.RoundSynthesis
		case protocol.RoundSynthesis:
			return protocol.RoundClosed
		default:
			return protocol.RoundClosed
		}
	}

	// Full parliamentary path.
	switch current {
	case protocol.RoundRegistering:
		return protocol.RoundPropose
	case protocol.RoundPropose:
		return protocol.RoundCritique
	case protocol.RoundCritique:
		return protocol.RoundRebuttal
	case protocol.RoundRebuttal:
		return protocol.RoundVote
	case protocol.RoundVote:
		return protocol.RoundExecute
	case protocol.RoundExecute:
		return protocol.RoundSynthesis
	case protocol.RoundSynthesis:
		return protocol.RoundClosed
	default:
		return protocol.RoundClosed
	}
}
