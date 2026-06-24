package main

import (
	"context"
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
		Propose:  parseDuration(envDefault("SWARM_TIMEOUT_PROPOSE", "150s"), 150*time.Second),
		Critique: parseDuration(envDefault("SWARM_TIMEOUT_CRITIQUE", "90s"), 90*time.Second),
		Rebuttal: parseDuration(envDefault("SWARM_TIMEOUT_REBUTTAL", "60s"), 60*time.Second),
		Vote:     parseDuration(envDefault("SWARM_TIMEOUT_VOTE", "300s"), 300*time.Second),
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

	// Start round advancement goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go roundAdvancer(ctx, machine)

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
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle /session/{id}/tokens sub-path.
		trimmed := strings.TrimPrefix(r.URL.Path, "/session/")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) != 2 || parts[1] != "tokens" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sessionID := parts[0]
		if sessionID == "" {
			http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

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
	fmt.Printf("SWARM_BUS_PORT=%d\n", port)
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

const minAdvanceBackoff = 5 * time.Second

func roundAdvancer(ctx context.Context, machine *state.Machine) {
	var timer *time.Timer
	resetTimer := func(advanceFailed bool) {
		if timer != nil {
			timer.Stop()
		}
		var remaining time.Duration
		if advanceFailed {
			// advanceRound didn't advance — avoid tight spin loop by backing off.
			remaining = minAdvanceBackoff
		} else {
			remaining = machine.RoundManager.TimeRemaining()
			if remaining <= 0 {
				remaining = minAdvanceBackoff
			}
		}
		timer = time.NewTimer(remaining)
	}

	resetTimer(false)
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return

		case <-timer.C:
			current := machine.RoundManager.Current()
			if current == protocol.RoundClosed {
				if timer != nil {
					timer.Stop()
				}
				return
			}

			machine.SessionRegistry.PruneStale(60 * time.Second)

			advanceFailed := false
			if machine.RoundManager.IsExpired() {
				prevRound := current
				advanceRound(machine)
				advanceFailed = machine.RoundManager.Current() == prevRound
			}
			resetTimer(advanceFailed)

		case <-machine.SubmitCh:
			// A session submitted data — eagerly check if we can advance early.
			current := machine.RoundManager.Current()
			if current == protocol.RoundClosed || current == protocol.RoundExecute || current == protocol.RoundSynthesis {
				continue
			}

			active := machine.SessionRegistry.ActiveCount()
			if active < 1 {
				continue
			}

			shouldAdvance := false

			if current == protocol.RoundVote {
				// VOTE phase: advance when >=75% of active sessions have voted.
				// 100% is ideal but max-effort sessions are slow; 75% gives a
				// strong consensus while tolerating one straggler.
				voteCount := len(machine.GetAllVotes())
				quorum := active * 3 / 4
				if quorum < 1 { quorum = 1 }
				if voteCount >= quorum {
					shouldAdvance = true
				}
			} else {
				// Other phases: use total submission count.
				submitted := machine.SubmissionCount()
				if submitted >= active {
					shouldAdvance = true
				}
			}

			if shouldAdvance {
				machine.SessionRegistry.PruneStale(60 * time.Second)
				advanceRound(machine)
				resetTimer(false)
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
		} else if machine.RoundManager.IsExpired() {
			// REGISTERING timed out with zero sessions — abort to CLOSED.
			fmt.Fprintf(os.Stderr, "[swarm-bus] ERROR: REGISTERING round timed out with 0 sessions registered — aborting\n")
			machine.RoundManager.ForceRound(protocol.RoundClosed)
		}
	case protocol.RoundPropose:
		submitted := machine.SubmissionCount()
		if submitted < 2 {
			// Deadlock breaker: if exactly 1 proposal after 3+ retries,
			// advance anyway so the pipeline doesn't stall forever.
			deadlock := machine.RoundManager.DeadlockCount()
			if submitted == 1 && deadlock >= 3 {
				fmt.Fprintf(os.Stderr, "[swarm-bus] PROPOSE stuck with 1 proposal after %d retries — advancing\n", deadlock)
				machine.RoundManager.Advance()
				return
			}
			machine.RoundManager.IncrementDeadlockCount()
			return // need at least 2 proposals to proceed
		}
		machine.RoundManager.Advance()
	case protocol.RoundCritique:
		machine.EliminateByFatalFlaw(0.5)
		machine.RoundManager.Advance()
	case protocol.RoundVote:
		voterPrefs := machine.GetVoterPrefs()
		activeProposals := machine.ActiveProposalIDs()
		if len(voterPrefs) > 0 && len(activeProposals) > 0 {
			result, err := protocol.TallyVotesIncremental(voterPrefs, activeProposals)
			if err == nil && result != nil {
				machine.SetVoteResult(result)
			}
		}
		// Retry on 0 votes: extend the round up to 2 times so slow
		// max-effort sessions have time to read proposals and vote.
		if len(voterPrefs) == 0 && machine.RoundManager.DeadlockCount() < 2 {
			machine.RoundManager.IncrementDeadlockCount()
			fmt.Fprintf(os.Stderr, "[swarm-bus] VOTE round had 0 votes — retrying (attempt %d/2)\n", machine.RoundManager.DeadlockCount())
			return // don't advance; timer will fire again
		}
		machine.RoundManager.Advance()
	case protocol.RoundExecute:
		machine.RoundManager.Advance()
	case protocol.RoundSynthesis:
		voteResult := machine.GetVoteResult()
		if voteResult != nil {
			fmt.Fprintf(os.Stderr, "[swarm-bus] synthesis — winner: %s, tally rounds: %d\n",
				voteResult.Winner, len(voteResult.RoundResults))
		}
		machine.RoundManager.Advance()
	case protocol.RoundClosed:
		return
	default:
		machine.RoundManager.Advance()
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
