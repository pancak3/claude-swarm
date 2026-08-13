package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/protocol"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/state"
	"github.com/anthropics/claude-code/plugins/claude-swarm/swarm-bus/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRequestBodySize = 1 << 20 // 1 MB limit to prevent DoS

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "swarm-bus: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse task brief from environment.
	taskBrief := &protocol.TaskBrief{
		TaskID:      envDefault("SWARM_TASK_ID", fmt.Sprintf("task-%d", time.Now().Unix())),
		Description: envDefault("SWARM_TASK_DESCRIPTION", "Solve the assigned task collaboratively"),
		SwarmSize:   envInt("SWARM_SIZE", 3),
		Depth:       0,
		MaxDepth:    5,
	}

	timeouts := protocol.RoundTimeouts{
		Register: parseDuration(envDefault("SWARM_TIMEOUT_REGISTER", "60s"), 60*time.Second),
		Propose:  parseDuration(envDefault("SWARM_TIMEOUT_PROPOSE", "180s"), 180*time.Second),
		Critique: parseDuration(envDefault("SWARM_TIMEOUT_CRITIQUE", "60s"), 60*time.Second),
		Rebuttal: parseDuration(envDefault("SWARM_TIMEOUT_REBUTTAL", "45s"), 45*time.Second),
		Vote:     parseDuration(envDefault("SWARM_TIMEOUT_VOTE", "120s"), 120*time.Second),
	}


	// Create shared state machine.
	machine := state.NewMachine(taskBrief.TaskID, taskBrief, timeouts, timeouts.Register)
	// Enable fast-path (3-round propose→vote→execute) by default.
	// Set SWARM_FAST_PATH=false for full parliamentary (critique+rebuttal).
	if envDefault("SWARM_FAST_PATH", "true") == "true" {
		machine.RoundManager.SetFastPath(true)
	}

	// P2.1: Load checkpoint if SWARM_CHECKPOINT_FILE is set and exists.
	if cpFile := os.Getenv("SWARM_CHECKPOINT_FILE"); cpFile != "" {
		if machine.LoadCheckpoint(cpFile) {
			fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint loaded from %s, resuming from round %s\n",
				cpFile, machine.RoundManager.Current())
		}
	}

	// Build shared MCP server (all sessions connect to this single instance).
	server := mcp.NewServer(
		&mcp.Implementation{Name: "swarm-bus", Version: "0.1.0"},
		&mcp.ServerOptions{},
	)

	server.AddTool(tools.RegisterTool(machine))
	server.AddTool(tools.SubmitProposalTool(machine))
	server.AddTool(tools.SubmitCritiqueTool(machine))
	server.AddTool(tools.SubmitRebuttalTool(machine))
	server.AddTool(tools.CastVoteTool(machine))
	server.AddTool(tools.ReadRoundTool(machine))
	server.AddTool(tools.GetStatusTool(machine))
	server.AddTool(tools.RegisterContractTool(machine))
	server.AddTool(tools.GetContractTool(machine))
	server.AddTool(tools.ReportTokensTool(machine))
	server.AddTool(tools.RoundDoneTool(machine))

	// Start round advancement goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go roundAdvancer(ctx, machine)

	// Safety timeout: absolute backstop (600s per round). Fires only if the
	// event-driven mechanism breaks (e.g., MCP deadlock, session that never
	// calls swarm_round_done and never gets pruned).
	go func() {
		safetyTimeout := parseDuration(envDefault("SWARM_SAFETY_TIMEOUT", "600s"), 600*time.Second)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var lastRound protocol.Round
		var roundStuckAt time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := machine.RoundManager.Current()
				if current == protocol.RoundClosed {
					return
				}
				if current != lastRound {
					lastRound = current
					roundStuckAt = time.Now()
				} else if time.Since(roundStuckAt) > safetyTimeout {
					fmt.Fprintf(os.Stderr, "[swarm-bus] SAFETY TIMEOUT: round %s stuck for %v — force-advancing\n",
						current, safetyTimeout)
					// Force-mark all remaining active sessions as done.
					for _, sid := range machine.SessionRegistry.GetActive() {
						machine.MarkSessionDone(sid)
					}
					machine.SessionRegistry.PruneStale(1 * time.Second)
					// Signal roundAdvancer to check again.
					machine.SubmitCh <- struct{}{}
				}
			}
		}
	}()

	// Handle graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	// Create shared HTTP handler for streamable MCP transport.
	getServer := func(r *http.Request) *mcp.Server {
		return server
	}
	handler := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{})

	mux := http.NewServeMux()
	mux.Handle("/mcp", limitRequestSize(handler))
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := machine.StatusSnapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := machine.StatusSnapshot()
		voteResult := machine.GetVoteResult()
		proposals := machine.GetProposals(false, "")
		eliminated := machine.GetEliminatedProposals()

		result := map[string]interface{}{
			"status":     snap,
			"tally":      voteResult,
			"proposals":  proposals,
			"eliminated": eliminated,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// /contract — shared code contract registry for P1.1
	mux.HandleFunc("/contract", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			entries := machine.GetContracts()
			if entries == nil {
				entries = []*protocol.ContractEntry{}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"entries": entries})
		case http.MethodPost:
			var entry struct {
				SessionID   string `json:"session_id"`
				ModuleName  string `json:"module_name"`
				ClassName   string `json:"class_name"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			if entry.ModuleName == "" && entry.ClassName == "" {
				http.Error(w, `{"error":"module_name or class_name required"}`, http.StatusBadRequest)
				return
			}
			machine.RegisterContract(protocol.ContractEntry{
					SessionID:   entry.SessionID,
					ModuleName:  entry.ModuleName,
					ClassName:   entry.ClassName,
					Description: entry.Description,
				})
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// POST /session/{id}/tokens — report token usage for a session.
	// POST /session/{id}/dead   — orchestrator declares a session dead (PID exited).
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/session/")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) < 2 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sessionID := parts[0]
		if sessionID == "" {
			http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
			return
		}
		subPath := parts[1]

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch subPath {
		case "tokens":
			var body struct {
				TokensIn  int64 `json:"tokens_in"`
				TokensOut int64 `json:"tokens_out"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			if !machine.SessionRegistry.UpdateTokens(sessionID, body.TokensIn, body.TokensOut) {
				http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case "dead":
			// Orchestrator notifies bus that a session's PID has exited.
			// Mark the session as done so it doesn't block round advancement.
			machine.SessionRegistry.Unregister(sessionID)
			machine.MarkSessionDone(sessionID)
			fmt.Fprintf(os.Stderr, "[swarm-bus] session %q declared dead by orchestrator\n", sessionID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	// Bus process cleanup is handled by the orchestrator script.
	// The orchestrator writes a PID file and kills stale processes before starting us.

	// Listen on a random available port on loopback only.
	// Support --port flag via SWARM_BUS_PORT env var (set by orchestrator)
	// or fallback to SWARM_BUS_ADDR for explicit address binding.
	bindAddr := "127.0.0.1:0"
	if p := envDefault("SWARM_BUS_PORT", ""); p != "" {
		bindAddr = "127.0.0.1:" + p
	} else {
		bindAddr = envDefault("SWARM_BUS_ADDR", "127.0.0.1:0")
	}
	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Print port to stdout so the orchestrator can capture it.
	// Explicitly flush — when stdout is redirected to a file, Go uses
	// block-buffering (4KB default), so without Sync() the port line
	// may sit in the buffer and the orchestrator times out waiting.
	fmt.Printf("SWARM_BUS_PORT=%d\n", port)
	os.Stdout.Sync()
	fmt.Fprintf(os.Stderr, "[swarm-bus] listening on port %d\n", port)
	fmt.Fprintf(os.Stderr, "[swarm-bus] task: %s\n", protocol.SanitizeLog(taskBrief.Description))

	httpServer := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		tools.StopRateLimiter()
		httpServer.Close()
	}()

	return httpServer.Serve(listener)
}

// limitRequestSize wraps an http.Handler with a per-request body size limit.
func limitRequestSize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		next.ServeHTTP(w, r)
	})
}


func roundAdvancer(ctx context.Context, machine *state.Machine) {
	// Event-driven round advancement: rounds advance when all active sessions
	// have signaled completion (via swarm_round_done) and the round's submission
	// quorum is met. No wall-clock timers — sessions self-report when done.
	//
	// Safety timeout: a goroutine fires after 600s per round as an absolute
	// backstop. It should never fire during normal operation; it exists only
	// for catastrophic failure (bus bug, network partition, MCP deadlock).

	for {
		select {
		case <-ctx.Done():
			return

		case <-machine.SubmitCh:
			current := machine.RoundManager.Current()
			if current == protocol.RoundClosed {
				return
			}

			// Prune sessions inactive for >180s (stuck sessions that
			// can't call swarm_round_done).
			pruned := machine.SessionRegistry.PruneStale(180 * time.Second)
			if pruned > 0 {
				fmt.Fprintf(os.Stderr, "[swarm-bus] pruned %d stale session(s)\n", pruned)
			}

			// RoundRegistering, RoundExecute, and RoundSynthesis
			// advance immediately — no swarm_round_done needed.
			if current == protocol.RoundRegistering ||
				current == protocol.RoundExecute ||
				current == protocol.RoundSynthesis {
				advanceRound(machine)
				continue
			}

			active := machine.SessionRegistry.ActiveCount()
			if active < 1 {
				continue
			}

			// Check if all active sessions have signaled done.
			if !machine.AllActiveSessionsDone() {
				// Not all done yet — wait for more signals.
				continue
			}

			// All sessions done — check if the round's submission quorum is met.
			shouldAdvance := false
			if current == protocol.RoundVote {
				voteCount := len(machine.GetAllVotes())
				quorum := active * 3 / 4
				if quorum < 1 { quorum = 1 }
				shouldAdvance = voteCount >= quorum
			} else {
				submitted := machine.SubmissionCount()
				quorum := active
				if quorum < 2 { quorum = 2 }
				shouldAdvance = submitted >= quorum
			}

			if shouldAdvance {
				advanceRound(machine)
			} else {
				// Quorum not met even though all sessions are done.
				// This means sessions abstained — advance anyway.
				if machine.SubmissionCount() >= 1 || current == protocol.RoundVote {
					fmt.Fprintf(os.Stderr, "[swarm-bus] all sessions done but quorum not met (%d submissions, %d active) — advancing\n",
						machine.SubmissionCount(), active)
					advanceRound(machine)
				}
			}
		}
	}
}

func advanceRound(machine *state.Machine) {
	current := machine.RoundManager.Current()

	switch current {
	case protocol.RoundRegistering:
		if machine.SessionRegistry.ActiveCount() >= 1 {
			machine.RoundManager.Advance()
		machine.ResetDoneSessions()
		}
	case protocol.RoundPropose:
		// Event-driven: roundAdvancer already verified all sessions done
		// AND quorum met. Just advance.
		submitted := machine.SubmissionCount()
		active := machine.SessionRegistry.ActiveCount()
		fmt.Fprintf(os.Stderr, "[swarm-bus] PROPOSE: %d proposals from %d active sessions — advancing\n", submitted, active)
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	case protocol.RoundCritique:
		machine.EliminateByFatalFlaw(0.5)
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	case protocol.RoundVote:
		voterPrefs := machine.GetVoterPrefs()
		activeProposals := machine.ActiveProposalIDs()
		if len(voterPrefs) > 0 && len(activeProposals) > 0 {
			result, err := protocol.TallyVotesIncremental(voterPrefs, activeProposals)
			if err == nil && result != nil {
				machine.SetVoteResult(result)
				// Evidence gate: a winner whose evidence was refuted becomes UNDECIDED.
				evidenceFloor := parseFloatDefault("SWARM_EVIDENCE_FLOOR", 0.5)
				if reason := machine.ApplyEvidenceGate(evidenceFloor); reason != "" {
					fmt.Fprintf(os.Stderr, "[swarm-bus] evidence gate: %s\n", reason)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "[swarm-bus] VOTE: %d votes, %d proposals — advancing\n", len(voterPrefs), len(activeProposals))
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	case protocol.RoundExecute:
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	case protocol.RoundSynthesis:
		voteResult := machine.GetVoteResult()
		if voteResult != nil {
			fmt.Fprintf(os.Stderr, "[swarm-bus] synthesis — winner: %s, tally rounds: %d\n",
				voteResult.Winner, len(voteResult.RoundResults))
		}
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	case protocol.RoundClosed:
		writeSnapshot(machine)
		return
	default:
		machine.RoundManager.Advance()
		machine.ResetDoneSessions()
	}

	newRound := machine.RoundManager.Current()
	result := protocol.RoundResult{
		Round:          newRound,
		SessionsActive: machine.SessionRegistry.ActiveCount(),
		Summary:        fmt.Sprintf("Advanced to %s", newRound),
	}
	data, _ := json.Marshal(result)
	fmt.Fprintf(os.Stderr, "[swarm-bus] round transition: %s\n", string(data))

	// Write checkpoint to disk after each transition.
	writeCheckpoint(machine, newRound)
}

// writeCheckpoint persists the current swarm state to a JSON checkpoint file.
// Writes to SWARM_CHECKPOINT_FILE if set, otherwise to stderr (for awareness).
// Only writes the most essential fields — lean, not comprehensive.
func writeCheckpoint(machine *state.Machine, round protocol.Round) {
	cpPath := os.Getenv("SWARM_CHECKPOINT_FILE")
	if cpPath == "" {
		return
	}

	snap := machine.StatusSnapshot()
	cp := map[string]interface{}{
		"round":       string(round),
		"task_id":     snap.TaskID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"sessions":    len(snap.Sessions),
		"proposals":   snap.ProposalsSubmitted,
		"votes":       snap.VotesCast,
		"winner":      snap.Winner,
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint marshal error: %v\n", err)
		return
	}

	if err := os.WriteFile(cpPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint write error: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "[swarm-bus] checkpoint written: %s\n", cpPath)
	}
}

// canonicalSnapshot is the schema-versioned, authoritative end-of-run artifact.
// The synthesizer reads this single file instead of re-parsing /status plus the
// bus stderr log plus per-session logs, removing divergence between sources.
type canonicalSnapshot struct {
	SchemaVersion int                   `json:"schema_version"`
	TaskID        string                `json:"task_id"`
	CompletedAt   string                `json:"completed_at"`
	Snapshot      *state.StatusSnapshot `json:"snapshot"`
}

// writeSnapshot persists the full swarm state on CLOSED as a canonical JSON
// plus a sha256 sidecar. Written unconditionally to SWARM_SNAPSHOT_FILE (if
// set); otherwise a no-op with a stderr note for awareness.
func writeSnapshot(machine *state.Machine) {
	snap := machine.StatusSnapshot()
	cs := canonicalSnapshot{
		SchemaVersion: 1,
		TaskID:        snap.TaskID,
		CompletedAt:   time.Now().UTC().Format(time.RFC3339),
		Snapshot:      snap,
	}

	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] snapshot marshal error: %v\n", err)
		return
	}

	path := os.Getenv("SWARM_SNAPSHOT_FILE")
	if path == "" {
		fmt.Fprintf(os.Stderr, "[swarm-bus] SWARM_SNAPSHOT_FILE unset — canonical snapshot not written\n")
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] snapshot write error: %v\n", err)
		return
	}

	sum := sha256.Sum256(data)
	sidecar := fmt.Sprintf("%x", sum)
	if err := os.WriteFile(path+".sha256", []byte(sidecar+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[swarm-bus] snapshot sidecar write error: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[swarm-bus] canonical snapshot written: %s (sha256 %s)\n", path, sidecar)
}

func parseFloatDefault(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func parseDuration(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
