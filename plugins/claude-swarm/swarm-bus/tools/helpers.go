package tools

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
)

// Per-prefix atomic counters for fast ID generation.
// Replaces crypto/rand-based genID to avoid syscall overhead per submission.
var idCounters sync.Map // map[prefix]*atomic.Uint64

func idCounter(prefix string) *atomic.Uint64 {
	c, _ := idCounters.LoadOrStore(prefix, new(atomic.Uint64))
	return c.(*atomic.Uint64)
}

// genID generates a short unique ID for submissions using an atomic counter.
// This is ~100x faster than the previous crypto/rand approach.
func genID(prefix string) string {
	n := idCounter(prefix).Add(1)
	return fmt.Sprintf("%s-%08d", prefix, n)
}

// parseArgs unmarshals raw arguments into a map.
func parseArgs(raw json.RawMessage) (map[string]interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}
	return args, nil
}

// getString extracts a string value from args.
func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// getStringSlice extracts a string slice from args.
func getStringSlice(args map[string]interface{}, key string) []string {
	if arr, ok := args[key].([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// getInt extracts an int value from args (JSON numbers come as float64).
func getInt(args map[string]interface{}, key string) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return 0
}

// checkAuth validates that the caller provided a valid session_id + auth_token pair.
// Returns an error message suitable for returning as an MCP error, or empty string on success.
func checkAuth(reg *state.SessionRegistry, sessionID, authToken string) string {
	if sessionID == "" {
		return "session_id is required"
	}
	if authToken == "" {
		return "auth_token is required for authenticated tools (call swarm_register first)"
	}
	if !reg.Authenticate(sessionID, authToken) {
		return "authentication failed: invalid session_id or auth_token"
	}
	return ""
}

// rateLimiter provides per-session token-bucket rate limiting with TTL-based cleanup.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     int     // tokens per second
	burst    int     // max burst
	ttl      time.Duration // idle TTL before bucket eviction
	stopCh   chan struct{}
	stopped  bool
}

type tokenBucket struct {
	tokens    float64
	lastCheck time.Time
}

const (
	defaultRateLimiterTTL   = 5 * time.Minute
	defaultRateLimiterRate  = 5
	defaultRateLimiterBurst = 10
)

func newRateLimiter(rate, burst int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    rate,
		burst:   burst,
		ttl:     defaultRateLimiterTTL,
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop evicts buckets that have been idle past the TTL.
func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictStale()
		case <-rl.stopCh:
			return
		}
	}
}

// evictStale removes buckets idle longer than ttl.
func (rl *rateLimiter) evictStale() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.ttl)
	for id, b := range rl.buckets {
		if b.lastCheck.Before(cutoff) {
			delete(rl.buckets, id)
		}
	}
}

// Stop terminates the background cleanup goroutine.
func (rl *rateLimiter) Stop() {
	rl.mu.Lock()
	if !rl.stopped {
		rl.stopped = true
		close(rl.stopCh)
	}
	rl.mu.Unlock()
}

// Allow checks if a session is allowed to proceed (rate-limited).
func (rl *rateLimiter) Allow(sessionID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[sessionID]
	if !ok {
		b = &tokenBucket{tokens: float64(rl.burst), lastCheck: time.Now()}
		rl.buckets[sessionID] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * float64(rl.rate)
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Global rate limiter: 5 calls per second, burst of 10 per session.
var globalRateLimiter = newRateLimiter(defaultRateLimiterRate, defaultRateLimiterBurst)

// StopRateLimiter stops the global rate limiter's background cleanup goroutine.
// Should be called during server shutdown to prevent goroutine leaks.
func StopRateLimiter() {
	globalRateLimiter.Stop()
}
