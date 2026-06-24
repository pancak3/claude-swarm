package tools

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
)

// =============================================================================
// genID tests
// =============================================================================

func TestGenIDReturnsUniqueSequentialIDs(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := genID("p")
		if ids[id] {
			t.Errorf("duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestGenIDDifferentPrefixes(t *testing.T) {
	pID := genID("p")
	cID := genID("c")
	vID := genID("v")
	rID := genID("r")

	if pID == cID || cID == vID || pID == vID || pID == rID {
		t.Error("different prefixes should produce different ID formats")
	}
}

func TestGenIDPrefixCounterIsolation(t *testing.T) {
	// Each prefix has its own counter.
	id1 := genID("x")
	id2 := genID("y")
	// First ID from each prefix should have counter 1.
	if id1 != "x-00000001" {
		t.Errorf("expected x-00000001, got %s", id1)
	}
	if id2 != "y-00000001" {
		t.Errorf("expected y-00000001, got %s", id2)
	}
}

// =============================================================================
// parseArgs tests
// =============================================================================

func TestParseArgsValidJSON(t *testing.T) {
	raw := json.RawMessage(`{"session_id": "s1", "value": 42}`)
	args, err := parseArgs(raw)
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if args["session_id"] != "s1" {
		t.Errorf("expected s1, got %v", args["session_id"])
	}
}

func TestParseArgsInvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{invalid}`)
	_, err := parseArgs(raw)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseArgsEmptyObject(t *testing.T) {
	raw := json.RawMessage(`{}`)
	args, err := parseArgs(raw)
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if len(args) != 0 {
		t.Errorf("expected empty map, got %d entries", len(args))
	}
}

// =============================================================================
// getString tests
// =============================================================================

func TestGetStringPresent(t *testing.T) {
	args := map[string]interface{}{"key": "value"}
	if got := getString(args, "key"); got != "value" {
		t.Errorf("expected 'value', got %q", got)
	}
}

func TestGetStringMissing(t *testing.T) {
	args := map[string]interface{}{}
	if got := getString(args, "key"); got != "" {
		t.Errorf("expected empty string for missing key, got %q", got)
	}
}

func TestGetStringWrongType(t *testing.T) {
	args := map[string]interface{}{"key": 42}
	if got := getString(args, "key"); got != "" {
		t.Errorf("expected empty string for non-string, got %q", got)
	}
}

// =============================================================================
// getStringSlice tests
// =============================================================================

func TestGetStringSliceValid(t *testing.T) {
	args := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}
	result := getStringSlice(args, "items")
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("unexpected values: %v", result)
	}
}

func TestGetStringSliceMissing(t *testing.T) {
	args := map[string]interface{}{}
	result := getStringSlice(args, "items")
	if result != nil {
		t.Errorf("expected nil for missing key, got %v", result)
	}
}

func TestGetStringSliceEmpty(t *testing.T) {
	args := map[string]interface{}{"items": []interface{}{}}
	result := getStringSlice(args, "items")
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

func TestGetStringSliceMixedTypes(t *testing.T) {
	args := map[string]interface{}{
		"items": []interface{}{"a", 42, "c"},
	}
	result := getStringSlice(args, "items")
	if len(result) != 2 {
		t.Errorf("expected 2 string items, got %d: %v", len(result), result)
	}
	if result[0] != "a" || result[1] != "c" {
		t.Errorf("unexpected values: %v", result)
	}
}

// =============================================================================
// getInt tests
// =============================================================================

func TestGetIntPresent(t *testing.T) {
	args := map[string]interface{}{"count": float64(42)}
	if got := getInt(args, "count"); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestGetIntZero(t *testing.T) {
	args := map[string]interface{}{"count": float64(0)}
	if got := getInt(args, "count"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestGetIntMissing(t *testing.T) {
	args := map[string]interface{}{}
	if got := getInt(args, "count"); got != 0 {
		t.Errorf("expected 0 for missing key, got %d", got)
	}
}

func TestGetIntNegative(t *testing.T) {
	args := map[string]interface{}{"count": float64(-5)}
	if got := getInt(args, "count"); got != -5 {
		t.Errorf("expected -5, got %d", got)
	}
}

// =============================================================================
// checkAuth tests
// =============================================================================

func newTestRegistry() *state.SessionRegistry {
	return state.NewSessionRegistry(30 * time.Second)
}

func TestCheckAuthEmptySessionID(t *testing.T) {
	reg := newTestRegistry()
	msg := checkAuth(reg, "", "token")
	if msg == "" {
		t.Error("expected error for empty session_id")
	}
}

func TestCheckAuthEmptyToken(t *testing.T) {
	reg := newTestRegistry()
	msg := checkAuth(reg, "s1", "")
	if msg == "" {
		t.Error("expected error for empty auth_token")
	}
}

func TestCheckAuthInvalidSession(t *testing.T) {
	reg := newTestRegistry()
	msg := checkAuth(reg, "nonexistent", "token")
	if msg == "" {
		t.Error("expected error for nonexistent session")
	}
}

func TestCheckAuthInvalidToken(t *testing.T) {
	reg := newTestRegistry()
	_, token, _ := reg.Register("s1", "correctness")
	msg := checkAuth(reg, "s1", "wrong-token")
	if msg == "" {
		t.Error("expected auth failure for wrong token")
	}
	_ = token
}

func TestCheckAuthSuccess(t *testing.T) {
	reg := newTestRegistry()
	_, token, _ := reg.Register("s1", "correctness")
	msg := checkAuth(reg, "s1", token)
	if msg != "" {
		t.Errorf("expected successful auth, got: %s", msg)
	}
}

// =============================================================================
// Rate limiter tests
// =============================================================================

func TestNewRateLimiterDefaults(t *testing.T) {
	rl := newRateLimiter(5, 10)
	if rl == nil {
		t.Fatal("rate limiter should not be nil")
	}
	if rl.rate != 5 || rl.burst != 10 {
		t.Errorf("unexpected config: rate=%d burst=%d", rl.rate, rl.burst)
	}
	rl.Stop()
}

func TestRateLimiterAllowInitially(t *testing.T) {
	rl := newRateLimiter(5, 10)
	defer rl.Stop()

	// Should allow burst of 10 initially.
	for i := 0; i < 10; i++ {
		if !rl.Allow("s1") {
			t.Errorf("request %d should be allowed (burst=10)", i+1)
		}
	}
	// 11th should be denied.
	if rl.Allow("s1") {
		t.Error("11th request should be denied (burst exhausted)")
	}
}

func TestRateLimiterTokenRefill(t *testing.T) {
	rl := newRateLimiter(100, 1) // high rate for fast refill
	defer rl.Stop()

	// Use the single burst token.
	if !rl.Allow("s1") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("s1") {
		t.Error("second request should be denied (burst=1)")
	}

	// Wait for token refill.
	time.Sleep(20 * time.Millisecond)

	// Should have refilled at least 1 token at rate=100/s.
	if !rl.Allow("s1") {
		t.Error("request after refill should be allowed")
	}
}

func TestRateLimiterPerSessionIsolation(t *testing.T) {
	rl := newRateLimiter(1, 1)
	defer rl.Stop()

	// s1 uses its token.
	if !rl.Allow("s1") {
		t.Fatal("s1 first request should be allowed")
	}
	if rl.Allow("s1") {
		t.Error("s1 second request should be denied")
	}

	// s2 should still have its token.
	if !rl.Allow("s2") {
		t.Error("s2 first request should be allowed (separate bucket)")
	}
}

func TestRateLimiterStop(t *testing.T) {
	rl := newRateLimiter(5, 10)
	rl.Stop()

	// Stop should be idempotent.
	rl.Stop()

	// Allow should still work after Stop (rate limiter just stops cleanup).
	if !rl.Allow("s1") {
		t.Error("Allow should still work after Stop")
	}
}

func TestRateLimiterEvictStaleKeepsActive(t *testing.T) {
	rl := newRateLimiter(5, 10)
	defer rl.Stop()

	rl.Allow("s1")
	rl.evictStale()

	rl.mu.Lock()
	_, exists := rl.buckets["s1"]
	rl.mu.Unlock()
	if !exists {
		t.Error("recent bucket should not be evicted")
	}
}

func TestStopRateLimiterGlobal(t *testing.T) {
	// Reset the global to a fresh one for deterministic testing.
	old := globalRateLimiter
	globalRateLimiter = newRateLimiter(5, 10)
	defer func() { globalRateLimiter = old }()

	StopRateLimiter()
	// Should be idempotent.
	StopRateLimiter()

	globalRateLimiter.mu.Lock()
	if !globalRateLimiter.stopped {
		t.Error("rate limiter should be stopped")
	}
	globalRateLimiter.mu.Unlock()
}

// =============================================================================
// idCounter tests
// =============================================================================

func TestIDCounterConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	seen := sync.Map{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := genID("p")
			if _, loaded := seen.LoadOrStore(id, true); loaded {
				t.Errorf("duplicate ID in concurrent use: %s", id)
			}
		}()
	}
	wg.Wait()
}
