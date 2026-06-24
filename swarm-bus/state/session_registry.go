package state

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
)

// SessionRegistry tracks all sessions connected to a swarm task.
type SessionRegistry struct {
	mu              sync.RWMutex
	sessions        map[string]*protocol.SessionInfo
	authTokens      map[string]string // session_id -> bearer token
	tokenByValue    map[string]string // bearer token -> session_id
	registerTimeout time.Duration
}

// NewSessionRegistry creates a registry with the given register timeout.
func NewSessionRegistry(registerTimeout time.Duration) *SessionRegistry {
	return &SessionRegistry{
		sessions:        make(map[string]*protocol.SessionInfo),
		authTokens:      make(map[string]string),
		tokenByValue:    make(map[string]string),
		registerTimeout: registerTimeout,
	}
}

// generateToken creates a cryptographically random bearer token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate auth token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Register adds a new session and generates a bearer auth token.
// Returns the session info and bearer token.
func (sr *SessionRegistry) Register(id, perspective string) (*protocol.SessionInfo, string, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if existing, ok := sr.sessions[id]; ok {
		return existing, "", fmt.Errorf("session %q already registered", id)
	}

	now := time.Now()
	si := &protocol.SessionInfo{
		ID:           id,
		RegisteredAt: now,
		LastSeen:     now,
		Active:       true,
		Perspective:  perspective,
	}
	sr.sessions[id] = si

	token, err := generateToken()
	if err != nil {
		return si, "", err
	}
	sr.authTokens[id] = token
	sr.tokenByValue[token] = id
	return si, token, nil
}

// Authenticate checks whether a session_id + token pair is valid using constant-time comparison.
func (sr *SessionRegistry) Authenticate(sessionID, token string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	expected, ok := sr.authTokens[sessionID]
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

// TokenForSession returns the bearer token for a given session ID (if any).
func (sr *SessionRegistry) TokenForSession(sessionID string) (string, bool) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	token, ok := sr.authTokens[sessionID]
	return token, ok
}

// Unregister removes a session and its auth token.
func (sr *SessionRegistry) Unregister(id string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if si, ok := sr.sessions[id]; ok {
		si.Active = false
	}
	if token, ok := sr.authTokens[id]; ok {
		delete(sr.tokenByValue, token)
		delete(sr.authTokens, id)
	}
}

// Heartbeat updates the LastSeen timestamp.
func (sr *SessionRegistry) Heartbeat(id string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if si, ok := sr.sessions[id]; ok {
		si.LastSeen = time.Now()
	}
}

// ActiveCount returns the number of active sessions.
func (sr *SessionRegistry) ActiveCount() int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	count := 0
	for _, si := range sr.sessions {
		if si.Active {
			count++
		}
	}
	return count
}

// GetActive returns all active session IDs.
func (sr *SessionRegistry) GetActive() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	ids := make([]string, 0, len(sr.sessions))
	for _, si := range sr.sessions {
		if si.Active {
			ids = append(ids, si.ID)
		}
	}
	return ids
}

// GetInfo returns session info by ID.
func (sr *SessionRegistry) GetInfo(id string) (*protocol.SessionInfo, bool) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	si, ok := sr.sessions[id]
	return si, ok
}

// PruneStale deactivates sessions that haven't sent a heartbeat within the timeout.
func (sr *SessionRegistry) PruneStale(timeout time.Duration) int {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	cutoff := time.Now().Add(-timeout)
	pruned := 0
	for _, si := range sr.sessions {
		if si.Active && si.LastSeen.Before(cutoff) {
			si.Active = false
			pruned++
		}
	}
	return pruned
}

// AllSessions returns info for all registered sessions (active and inactive).
func (sr *SessionRegistry) AllSessions() []protocol.SessionInfo {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	result := make([]protocol.SessionInfo, 0, len(sr.sessions))
	for _, si := range sr.sessions {
		result = append(result, *si)
	}
	return result
}

// AllRegistered checks if all expected sessions have registered.
func (sr *SessionRegistry) AllRegistered(expected int) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.ActiveCount() >= expected
}

// AllAuthTokens returns a copy of all auth tokens (for checkpoint serialization, P2.1).
func (sr *SessionRegistry) AllAuthTokens() map[string]string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	cp := make(map[string]string, len(sr.authTokens))
	for k, v := range sr.authTokens {
		cp[k] = v
	}
	return cp
}

// RestoreSession adds a session from checkpoint data (P2.1).
func (sr *SessionRegistry) RestoreSession(si protocol.SessionInfo) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.sessions[si.ID] = &si
}

// UpdateTokens updates the token usage for a session.
// Returns true if the session was found and updated.
func (sr *SessionRegistry) UpdateTokens(sessionID string, tokensIn, tokensOut int64) bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	si, ok := sr.sessions[sessionID]
	if !ok {
		return false
	}
	si.TokensIn += tokensIn
	si.TokensOut += tokensOut
	return true
}

// RestoreAuthToken restores an auth token from checkpoint data (P2.1).
func (sr *SessionRegistry) RestoreAuthToken(sessionID, token string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.authTokens[sessionID] = token
	sr.tokenByValue[token] = sessionID
}
