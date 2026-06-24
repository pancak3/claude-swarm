# Claude-Swarm Debate Skill — Improvement Advice

**Purpose:** Actionable improvement plan for an agent that will edit the `claude-swarm` skill.
**Provenance:** Synthesized from an 11-session parliamentary debate, grounded in ~8 real debate runs over two days. Two independent session plans (correctness + security perspectives) converged on the same root causes and the same files; that cross-session agreement is the strongest validity signal this tool produces.
**Verification:** Claims marked **[verified]** were re-confirmed against the live source on 2026-06-22. Line numbers drift; the *mechanism* is correct, but re-confirm exact line numbers before editing.

---

## How the tool actually performed (the evidence base)

1. **Vote degeneracy.** First run reported `Votes cast: 0` / "no winner determined". **Every** run printed `Low vote diversity (Gini=0 < 0.3)`.
2. **Workspace pollution.** Worker sessions ignored an explicit "do NOT edit any files" instruction and edited real target files, leaving stray backups (`paper.tex.bak_s1`, `paper.tex.s5_backup`, `paper.tex.s8_final`). The orchestrator had to back up, diff, and clean up after every run.
3. **Untrustworthy winner.** The IRV winner sometimes contained fabrications its own `Risks` field flagged (an invented threshold, a double-counted term in a generated algorithm). The winner needs over-reading against source every time.
4. **Bad ROI on small tasks.** ~3 min, $0.05–$0.19, 80k–334k input tokens per run, even for a column reorder or a two-line reasoning question.
5. **Sessions are blind to orchestrator context.** Grounding evidence had to be pasted into the prompt manually.

---

## KEEP — what works and must not be lost

- **Multi-perspective deliberation** (correctness / simplicity / performance / security). Genuinely strong on divergent analysis: multi-document diagnosis, terminology policy, prioritized cut lists.
- **Cross-session convergence as a trust signal.** When independent sessions agree, it is a strong validity indicator. (This very plan is an example: two sessions found the same bugs at the same lines.)
- **Session output isolation** at `.swarm-state/<run>/sessions/<id>/output/`. The mechanism exists and works; the problem is sessions are not *forced* to use it.
- **Fatal-flaw elimination** requiring ≥2 critiquing sessions (`tally.go`), and **self-vote prevention** (`machine.go`).
- **Checkpoint/resume** and **token-budget tracking** in `swarm-orchestrate.sh`.

---

## P0 — Safety (prevents corruption and pollution)

### P0.1 Enforce read-only mode for analysis/debate workers
- **Problem:** Workers edit real files despite "do not edit." Prompt text is not enforcement.
- **Root cause [verified]:** `scripts/swarm-spawn.sh:83-85` launches `claude` with `--dangerously-skip-permissions --allow-dangerously-skip-permissions --permission-mode bypassPermissions`, giving unrestricted filesystem write access to every worker.
- **Fix (layered):**
  1. Add a `SWARM_MODE` (`analyze` | `implement`) env var, set by the orchestrator from task type. Debate defaults to `analyze`.
  2. In `swarm-spawn.sh`, when `SWARM_MODE=analyze`, drop `--dangerously-skip-permissions` and pass `--allowedTools "Bash,Read,Grep,Glob"` (strip Write/Edit), or `--permission-mode default`.
  3. Add a `PreToolUse` hook in `hooks/hooks.json` that rejects Write/Edit targeting paths outside the session's `output/` dir when `SWARM_MODE=analyze` (defense in depth, since `--allowedTools` support varies by CLI version).
  4. Add a `--read-only` flag to `swarm-orchestrate.sh`.
- **Files:** `scripts/swarm-spawn.sh`, `hooks/hooks.json`, `scripts/swarm-orchestrate.sh`, new `scripts/swarm-guard-readonly.sh`.
- **Effort:** Low–Medium. **Risk:** Medium — scope read-only to the analysis rounds; allow writes only in an explicit EXECUTE phase.

### P0.2 Confine scratch/backup files to `.swarm-state/` and auto-clean
- **Problem:** Stray `*.bak_s*` / `*_backup` / `*_final` files left in the project root.
- **Root cause:** Sessions use the project root as cwd; `scripts/swarm-cleanup.sh` only prunes `.swarm-state/` dirs (`-mtime +7`), never stray workspace files.
- **Fix:** export `TMPDIR="${RUN_DIR}/tmp"` at spawn; add a stray-backup scanner to `swarm-cleanup.sh` that quarantines (moves, not deletes) matching files into `.swarm-state/artifacts/`; extend the `cleanup()` trap in `swarm-orchestrate.sh` to log/quarantine backups created during the run.
- **Files:** `scripts/swarm-orchestrate.sh` (spawn + trap), `scripts/swarm-cleanup.sh`.
- **Effort:** Low. **Risk:** Low (quarantine, no deletion).

---

## P1 — Correctness (prevents wrong outputs and metrics)

### P1.1 Fix hardcoded `SwarmSize`, zero-vote advance, and session accounting
- **Problem:** `Votes cast: 0`; inconsistent counts like `8 active / 3 total`, `14 active / 3 total`.
- **Root causes [verified]:**
  - `swarm-bus/main.go:35` → `SwarmSize: 3` is **hardcoded**, never read from the orchestrator's `SWARM_SIZE` (default 8). `StatusSnapshot` (`state/machine.go:~431`) derives `TotalSessions` from this static 3, so "active / total" is always incommensurate with the live registry count.
  - Round-advance logic can advance PROPOSE with too few proposals, leaving VOTE with 0 ballots and no tally.
- **Fix:**
  1. `main.go`: `SwarmSize: envInt("SWARM_SIZE", 3)`; export `SWARM_SIZE` to the bus from `swarm-orchestrate.sh`.
  2. `state/machine.go` `StatusSnapshot`: set `TotalSessions = len(SessionRegistry.AllSessions())` so both numbers come from one source.
  3. `main.go` PROPOSE→VOTE: add a quorum guard; if `<min(2, active)` proposals at timeout, retry rather than advance.
  4. VOTE handler: on 0 votes, retry with extended timeout (bounded, e.g. 2 attempts) and emit a clear diagnostic instead of silently producing "no winner."
  5. `session_registry.go`: `PurgeInactive()` to remove (not just deactivate) stale sessions so `AllSessions()` does not bloat across runs.
- **Files:** `swarm-bus/main.go`, `swarm-bus/state/machine.go`, `swarm-bus/state/session_registry.go`, `scripts/swarm-orchestrate.sh`. (Go recompile required.)
- **Effort:** Medium. **Risk:** Medium (round-timing changes need testing; lock order `proposalsMu → critiquesMu → votesMu` is documented — preserve it).

### P1.2 Fix the Gini diversity metric (always reports 0)
- **Problem:** `Low vote diversity (Gini=0)` on every run, including healthy debates.
- **Root cause [verified]:** `swarm-bus/protocol/tally.go:89` **and** `:222` compute `DiversityScore = GiniCoefficient(RoundResults[len-1].CandidateVotes)`, i.e. the **final** IRV round after eliminations, where votes are maximally concentrated (often a single survivor → Gini 0).
- **Fix:** compute Gini from `RoundResults[0]` (first round = true initial spread) in both `TallyVotes` and `TallyVotesIncremental`. Add a `DegenerateVote` flag for the single-candidate case so the orchestrator can print "single surviving candidate" instead of the misleading low-diversity warning, and skip the warning when `votes == 0`.
- **Files:** `swarm-bus/protocol/tally.go`, warning text in `scripts/swarm-orchestrate.sh`.
- **Effort:** Low. **Risk:** Low (backward-compatible).

### P1.3 Surface self-flagged risks and verify the winner
- **Problem:** Winner emitted as authoritative despite self-flagged fabrications.
- **Root cause:** `synthesize_results()` in `swarm-orchestrate.sh` prints `winner_risks` as a flat string and never checks them against the winner's own content.
- **Fix:** after extracting `winner_risks`, print a prominent "VERIFY BEFORE APPLYING" block (one risk per line); recommend re-reading source. Optional `--require-verification` flag that falls back to the runner-up when a risk's keywords match the winner's own approach/architecture text. Add a prompt-template line: "Your Risks field will be checked against your output; flag uncertainty honestly."
- **Files:** `scripts/swarm-orchestrate.sh`, `templates/swarm-prompt.md`, optionally `swarm-bus/protocol/schema.go` (`VerificationWarnings`).
- **Effort:** Low–Medium. **Risk:** Low (additive display).

---

## P2 — Efficiency & ergonomics

### P2.1 Triviality gate (skip the swarm for mechanical tasks)
- **Problem:** Full swarm spawned for a column reorder / yes-no question.
- **Root cause [verified]:** `scripts/swarm-assess.sh:21` returns a floor of `"3"`; it never returns a skip signal, and `swarm-intercept.sh` / `swarm-orchestrate.sh` always spawn whatever size it returns.
- **Fix:** add a structural pre-check in `swarm-assess.sh` — if `word_count ≤ ~15–30` AND no decision verbs (`choose|decide|design|which|compare|evaluate|should|better|trade-off`) AND no multi-file signals, return `"0"`. Handle `0` in `swarm-intercept.sh` / `swarm-orchestrate.sh` by skipping the swarm and letting a single worker handle it. Add `--force-swarm` to override.
- **Files:** `scripts/swarm-assess.sh`, `scripts/swarm-intercept.sh`, `scripts/swarm-orchestrate.sh`.
- **Effort:** Low. **Risk:** Low (multi-condition AND is conservative; `--force-swarm` is the escape hatch).

### P2.2 Pass orchestrator context to sessions
- **Problem:** Sessions cannot see the orchestrator's conversation; evidence is pasted manually and re-derived per session.
- **Root cause:** `swarm-spawn.sh` substitutes only `{SESSION_ID}`, `{SWARM_SIZE}`, `{PERSPECTIVE}`, `{TASK_DESCRIPTION}`, `{SESSION_OUTPUT_DIR}`; `TaskBrief` has no context field.
- **Fix:** orchestrator writes `${RUN_DIR}/context.json` (constraints + grounding evidence); `swarm-spawn.sh` injects it via a new `{CONTEXT}` template placeholder; add `--context-file` to the orchestrator and an optional `Context` field on `TaskBrief` (`omitempty`, backward-compatible).
- **Files:** `scripts/swarm-orchestrate.sh`, `scripts/swarm-spawn.sh`, `templates/swarm-prompt.md`, optionally `swarm-bus/protocol/schema.go`.
- **Effort:** Low. **Risk:** Low.

---

## Recommended execution order

`P1.2` (Gini, trivial) → `P1.1` (SwarmSize/accounting/zero-vote) → `P0.1` (read-only) → `P0.2` (backup confinement) → `P1.3` (risk verification) → `P2.1` (triviality gate) → `P2.2` (context passthrough).

Rationale: land the cheap correctness fixes first (Gini, SwarmSize), then the safety net (read-only + cleanup), then verification, then efficiency. All items are independent except the state-consistency work, which depends on the `SwarmSize` fix.

## Caveat for the implementing agent

Line numbers in this document were reported by debate sessions reading the source and spot-checked against the live tree; they drift with edits. Re-confirm each `file:line` before editing, and after the read-only fix (P0.1) re-run a debate with a deliberate "edit a file" instruction to confirm workers can no longer mutate the workspace.
