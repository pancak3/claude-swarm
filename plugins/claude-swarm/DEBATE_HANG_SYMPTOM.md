# Debate Skill — Post-Registration-Fix Hang (Symptom Report)

**Plugin:** claude-swarm **version 0.9.0**
**Date observed:** 2026-06-22
**Status:** Registration is FIXED. The infinite hang (H1) is now FIXED too. Remaining: the run completes but yields **0 votes / no winner** because the registration gate advances on the *first* session.
**Companion doc:** see `DEBATE_SKILL_IMPROVEMENTS.md` (this is the live symptom for its P1.1/P1.2 items).

---

## UPDATE — 2026-06-22 17:46 (re-verified after a fix landed)

A fix landed in `scripts/swarm-orchestrate.sh` (modified 17:09). Re-tested with a bounded size-3 run; results:

- **H1 (infinite hang): FIXED and verified.** Line 441-445 now polls the bus with `WAIT_TIMEOUT` (default 1800s) instead of waiting on PIDs forever. The test logged `Waiting for sessions to complete (polling bus, timeout=1800s)...` then `Bus reports CLOSED — sessions complete` after ~2 min and **exited on its own**. No more manual TaskStop.
- **H2 (synthesize on 0 votes): partially addressed.** Line ~724 now has `# H2: when votes==0, synthesize from proposals anyway`, but the run still printed `(no winner — vote tally did not produce a result)` and `Votes cast: 0`. With only one effective proposer (see below) the fallback has nothing meaningful to synthesize.

### NEW PRIMARY ROOT CAUSE — registration gate advances on the first session
`scripts/swarm-orchestrate.sh:421-428`:
```bash
for try in $(seq 1 ${REG_TRIES}); do
  registered=$(curl -sf ... '.active_sessions // 0' ...)
  if [ "${registered}" -ge 1 ]; then        # <-- advances as soon as ONE registers
    event "${registered} session(s) registered."
    break
  fi
  sleep 0.5
done
```
The gate breaks at `registered >= 1`, so the orchestrator proceeds the instant the first session registers. The other sessions register a few seconds later (a separate run's `/status` showed 7 of 8 active *after* the gate), but by then PROPOSE/VOTE has already advanced. Result: effectively one proposer, one proposal, self-voting blocked, **0 votes, no winner** — every time.

**Fix:** wait for a quorum before advancing, not just one session. E.g. break when `registered >= SWARM_SIZE`, or when the count has been stable for ~2-3 polls, or `registered >= max(2, ceil(0.6 * SWARM_SIZE))`, whichever comes first; keep the existing timeout as the hard cap; and abort/synthesize-from-proposals only if `registered < 2` at timeout. A vote needs at least 2 distinct proposers (self-vote is blocked), so the gate must guarantee ≥2 before PROPOSE.

This single change should convert the current "completes-but-empty" behavior into a real debate with votes and a winner.

---

## UPDATE — 2026-06-22 18:09 — RESOLVED with adequate phase timeouts (root cause confirmed)

The quorum-gate fix landed (orchestrate 17:54): the gate now waits for `>= 60% of SWARM_SIZE` (min 2) and a stable count. Verified: `2/4 session(s) registered (quorum met, stable)`. Registration, hang, and quorum are all fixed.

The remaining 0-vote problem is caused by **phase timeouts that are too short for max-effort sessions.** Defaults (`swarm-bus/main.go:41-45`): Register 30s, **Propose 30s**, Critique 45s, Rebuttal 30s, **Vote 20s**. Sessions are spawned at `SWARM_EFFORT=max` (`swarm-spawn.sh:~46`) and cannot read the task, think, submit a proposal, then read peers and cast a ranked vote within those windows. The bus advances on its timers faster than the `claude -p` sessions progress, so proposals/votes land in already-closed phases.

**Confirmed by controlled experiment (same task, same size 4):**
- Default timeouts: `Proposals submitted: 2, Votes cast: 0` -> `(no winner)`.
- `--phase-sizes REGISTER=60,PROPOSE=150,CRITIQUE=90,VOTE=90`: `Proposals submitted: 3, Votes cast: 3` -> a real Winning Proposal.

The only variable changed was the phase timeouts.

**Immediate workaround (works today):**
```bash
bash scripts/swarm-orchestrate.sh --phase-sizes REGISTER=60,PROPOSE=150,CRITIQUE=90,VOTE=90 "<task>" <N>
```

**Proper fix:** raise the default phase timeouts in `swarm-bus/main.go:41-45` (e.g., Propose 150s, Critique 90s, Rebuttal 60s, Vote 90s), or scale them with `SWARM_EFFORT` (short for low, long for max), or lower the default `SWARM_EFFORT` for fan-out sessions. Each phase timeout must exceed the per-step latency of a single max-effort session.

With adequate timeouts the swarm is functional end-to-end: register (quorum) -> propose -> critique -> vote -> synthesis -> winner, terminating on its own.

---

## UPDATE — 2026-06-22 18:33 — NEW REGRESSION: analyze-mode default blocks MCP registration

After a `plugin reload`, the default spawn mode became `analyze` (read-only), and the swarm regressed to **0 sessions registered -> abort**, even though the same code produced 3 votes + a winner at 18:03 (which ran in the old `implement`/bypass default).

**Definitive evidence:**
- `RUN/proxy.log`: steady `POST /v1/messages 200` responses — the session is alive and the API works.
- `RUN/s1.log` (the whole session output): *"I need permission to access the swarm bus tools to participate... Please approve the `mcp__swarm-bus__swarm_register` call so I can register..."*
- `RUN/bus.log`: `ERROR: REGISTERING round timed out with 0 sessions registered — aborting`.

**Root cause:** read-only enforcement is implemented by *removing* `--dangerously-skip-permissions`:
- `scripts/swarm-spawn.sh:87` `SWARM_MODE="${SWARM_MODE:-analyze}"` (analyze is now default); `scripts/swarm-orchestrate.sh:19` same default, exported at line 370.
- `scripts/swarm-spawn.sh:84-88`: only `implement` mode passes `--dangerously-skip-permissions`; analyze mode passes neither bypass nor an MCP allowlist (the comment says "We cannot use --allowedTools because it blocks MCP tools").
- Consequence: in analyze mode the session must get interactive approval to call `mcp__swarm-bus__swarm_register`. In `claude -p` (non-interactive) there is no approver, so it blocks and never registers.

The author swapped one MCP-blocking mechanism (an allowlist that omitted MCP) for another (no bypass at all). Both prevent registration.

**Fix:** in analyze mode, explicitly allow the bus MCP tools while denying file mutation. The MCP server is named `swarm-bus` (see `RUN/mcp-config.json`), so:
```bash
# analyze (read-only) branch in swarm-spawn.sh:
EXTRA_ARGS+=(--permission-mode default)
EXTRA_ARGS+=(--allowedTools "Bash,Read,Grep,Glob,WebSearch,WebFetch,mcp__swarm-bus")
EXTRA_ARGS+=(--disallowedTools "Write,Edit,NotebookEdit")
```
`--allowedTools` does NOT block MCP tools when the MCP server/tool is listed — the prefix `mcp__swarm-bus` (or the explicit tool names `mcp__swarm-bus__swarm_register`, `..._submit_proposal`, `..._cast_vote`, `..._read_round`, `..._get_status`) auto-allows them. This keeps the workspace read-only AND lets sessions register/propose/vote.

**Immediate workaround (works today):** run in implement mode, which keeps `--dangerously-skip-permissions` and therefore allows the MCP tools. With the timeout defaults now fixed, no `--phase-sizes` is needed:
```bash
bash scripts/swarm-orchestrate.sh --implement "<task>" <N>
```
Note: the `/swarm-debate` skill calls orchestrate WITHOUT `--implement`, so it currently inherits the broken analyze default. Until the analyze-mode allowlist fix lands, use `--implement` (or `SWARM_MODE=implement`).

---

## UPDATE — 2026-06-22 18:50 — RESOLVED: swarm works end-to-end at defaults

The analyze-mode allowlist fix landed (`scripts/swarm-spawn.sh:89-90`): analyze mode now passes `--permission-mode default` with `--allowedTools "Bash,Read,Grep,Glob,WebSearch,WebFetch,mcp__swarm-bus"`, so read-only sessions can still call the bus MCP tools.

Verified with a **no-flag** run (default analyze mode, default timeouts, size 4):
```
Swarm mode: analyze
2/4 session(s) registered (quorum met, stable)
Bus reports CLOSED — sessions complete            (~5 min, exits on its own)
Proposals submitted: 4, Votes cast: 1  -> a Winning Proposal (not "no winner")
```
All four core bugs are now fixed: registration, infinite hang, register-gate-at-1 (quorum), and the 0-vote/short-timeout issue. `/swarm-debate` (analyze default) now produces a result without flags.

### Residual polish item (not blocking)
Vote participation is low: only **1 of 4** proposers cast a vote, so the winner rests on few ballots. The VOTE phase still does not capture all sessions before it closes (some are likely still finishing earlier phases at `SWARM_EFFORT=max`). Options if higher vote diversity is wanted: raise `SWARM_TIMEOUT_VOTE` further, have the bus wait for votes from a quorum of active sessions before tallying (not just a timer), or lower default effort so sessions reach the vote step sooner. The run still produces a winner, so this is quality, not correctness.

---

---

## TL;DR

After the registration fix, sessions register and submit proposals, but the run never produces a result: the bus round goes to `CLOSED` with **0 votes**, no winner, and no synthesis, while the orchestrator hangs on `Waiting for N session(s) to complete` and must be killed manually. The submitted "proposals" are the sessions' *restated plans*, not their analysis.

---

## Reproduction

```bash
cd <a project dir>
SWARM_TIMEOUT_REGISTER=60 bash /home/debian/gits/claude-code/plugins/claude-swarm/scripts/swarm-orchestrate.sh \
  "Find semantic errors in <some file>. Output a ranked list." 8
```

Observed on a real run: `RUN_DIR = .swarm-state/swarm-20260622-133944` (artifacts preserved there).

---

## Observed symptom chain (verbatim evidence)

1. Spawn logs an odd role/size mismatch:
   ```
   Role pool: 15 sessions from roles (using user size: 8)
   API proxy started on port 37697 ... per-session temp/top_p/max_tokens active
   Swarm Bus started on port 37973
   All 8 sessions spawned. Waiting for registration...
   Waiting up to 60s for sessions to register...
   ```
2. The registration counter **undercounts at the timeout boundary**, then the orchestrator waits on the **spawn target (8)**, not the registered count:
   ```
   1 session(s) registered.                 <-- read once at the 60s checkpoint; misleading
   Checkpoint: phase=REGISTER
   Waiting for 8 session(s) to complete...   <-- HANGS HERE INDEFINITELY
   ```
   Live bus `/status` ~7 min later showed **7** sessions actually active (not 1), 2 proposals, **0 votes**, round `CLOSED`:
   ```json
   { "active": 7, "proposals": 2, "votes": 0, "phase": null, "round": "CLOSED" }
   ```
3. `checkpoint.json` is frozen at `"phase": "PROPOSE"` — the run never reached VOTE/SYNTHESIS.
4. **0 files** were written under `RUN_DIR/sessions/*/output/`.
5. The 2 submitted proposals were the sessions' *plans*, e.g.:
   > "Read the full paper.tex line by line. For each ... quote ... CONFIRM or REFUTE ... Output a severity-ranked list ..."
   i.e., methodology text, not findings.
6. The orchestrator never exits on its own; it had to be stopped with TaskStop.

Net effect: the swarm starts but produces **no usable output**.

---

## Root-cause hypotheses (ranked, with file pointers)

### H1 — Completion-wait blocks on the spawn target, not the registered count, with no timeout *(most likely the hang)*
The orchestrator prints `Waiting for N session(s) to complete` using the requested swarm size (8). When fewer register (7 here), it waits forever for the missing one.
- **Fix:** wait on the *registered/active* count from the bus `/status`, and add a hard wall-clock timeout that forces advance to synthesis.
- **Where:** the completion-wait loop in `scripts/swarm-orchestrate.sh` (the block that emits `Waiting for ... session(s) to complete`); the active/total accounting in `swarm-bus/state/machine.go` `StatusSnapshot` (and the hardcoded `SwarmSize: 3` at `swarm-bus/main.go:~35`, which still poisons `TotalSessions`).

### H2 — Round CLOSES with 0 votes and yields no winner/synthesis *(degenerate vote)*
`/status` shows `votes: 0, round: CLOSED`. The pipeline advanced past PROPOSE/VOTE without any votes and produced no winner or synthesis.
- **Fix:** require a vote quorum to advance; on 0 votes, retry the VOTE phase with an extended timeout; if still 0, synthesize from the submitted proposals instead of hanging/closing empty.
- **Where:** the VOTE handling in `swarm-bus/main.go` (advance logic) and the synthesis trigger in `scripts/swarm-orchestrate.sh`. (Same as `DEBATE_SKILL_IMPROVEMENTS.md` P1.1.)

### H3 — Sessions submit their plan as the proposal instead of completed analysis
The proposals were methodology restatements, suggesting the PROPOSE deadline fires before sessions finish the actual work (they run at `SWARM_EFFORT=max` by default, which is slow), or the prompt/protocol asks for a proposal before the analysis exists.
- **Fix:** lengthen the PROPOSE timeout relative to effort, or make the prompt require the *result* (not the plan) as the proposal; consider lowering default `SWARM_EFFORT` for fan-out sessions.
- **Where:** phase timeouts in `scripts/swarm-orchestrate.sh`; the proposal instruction in `templates/swarm-prompt.md`; `SWARM_EFFORT` default in `scripts/swarm-spawn.sh:~45`.

### H4 — Role-pool count mismatch *(lower priority, flag it)*
`Role pool: 15 sessions from roles (using user size: 8)` builds a 15-entry per-session config (`s1..s15`) while only `s1..s8` spawn. Per-session params may be mis-indexed for some sessions.
- **Where:** the role-pool block in `scripts/swarm-orchestrate.sh:~151-227` and `config/roles.json`.

### H5 — Misleading registration log *(cosmetic but confusing)*
`1 session(s) registered` is read once at the timeout boundary and undercounts the eventual 7. Report the count at advance time, or poll until stable.
- **Where:** the registration wait/log in `scripts/swarm-orchestrate.sh:~417-432`.

---

## Verification recipe (after fixing)

1. Run the reproduction command above with a deliberate analysis task.
2. Confirm the log shows the real registered count (e.g., `7/8`) and the run **advances to VOTE and SYNTHESIS** rather than printing `Waiting for 8 session(s) to complete` and hanging.
3. Confirm `/status` shows `votes > 0` (or, if 0, that the run still synthesizes from proposals) and a `Winning Proposal` block is printed.
4. Confirm `RUN_DIR/sessions/*/output/` contains actual findings, not plan restatements.
5. Confirm the orchestrator **exits on its own** within the expected wall-clock budget (no manual TaskStop needed).

## Caveat for the implementing agent
Line numbers drift; re-confirm each `file:line` against the current source before editing. The preserved failing run is at `.swarm-state/swarm-20260622-133944/` (`checkpoint.json` shows `phase=PROPOSE`).
