# Claude Swarm — Fix Plan

作成日: 2026-06-28

---

## Phase 1: Critical fixes from swarm test

| Task | 内容 | DoD | Depends | Status |
|------|------|-----|---------|--------|
| 1.1 | Fix MCP permission denial in analyze mode | spawn.sh uses bypassPermissions with --allowedTools gate | - | cc:完了 |
| 1.2 | Fix swarm-htop.sh zsh compatibility | `status` variable renamed, `fmt_cost` issues fixed | - | cc:完了 |
| 1.3 | Fix orchestrator synthesis — empty session logs | synthesis reads participation from bus API instead of file logs | 1.1 | cc:完了 |
| 1.4 | Fix session graceful exit detection | prompt.md instructs sessions to stop on CLOSED/SYNTHESIS | - | cc:完了 |

## Phase 2: Plugin distribution

| Task | 内容 | DoD | Depends | Status |
|------|------|-----|---------|--------|
| 2.1 | Bump version and update plugin manifest | version bumped in package.json / plugin.yaml | 1.1-1.4 | cc:完了 |
| 2.2 | Build swarm-bus binary | `go build` succeeds, binary committed or buildable | 1.1 | cc:完了 |
| 2.3 | Commit + push to repo | `git push origin main` succeeds | 2.1, 2.2 | cc:完了 |
| 2.4 | Verify install from marketplace | Fresh install works, swarm test passes | 2.3 | cc:完了 |

---

## Spec delta

### Problem

Swarm sessions running `claude -p` with `--permission-mode default` cannot use MCP tools because non-interactive mode denies tool permission prompts. Sessions register with the bus but produce no text output, leaving 0-byte log files. The orchestrator synthesis relies on session logs for conclusions, which are empty.

### Fix

1. **swarm-spawn.sh L89**: Change `--permission-mode default` to `--permission-mode bypassPermissions`. The `--allowedTools` list already restricts to Bash,Read,Grep,Glob,WebSearch,WebFetch,mcp__swarm-bus — Write/Edit are NOT in this list, so the read-only safety is preserved.

2. **swarm-orchestrate.sh**: Remove session-log-based conclusion extraction (L865-915 in old code). Replace with bus-API-based participation summary (proposal count, vote record per session). Already partially done in the working tree diff.

3. **swarm-htop.sh L35**: Rename `data`/`status` variables to avoid zsh reserved words. Rename `time` usage to `timeleft` (already done partially — L45 uses `timeleft` but `time` was used elsewhere). Actually check: the function-local variables `status` and `time` don't conflict in bash, only the `status` attempt at the monitor level. The htop.sh itself is fine — the issue is in the monitor scripts from the CLI session. But `htop.sh` should still be robust.

4. **swarm-prompt.md**: Add explicit CLOSED/SYNTHESIS exit instruction (already in working tree diff).

### Spec skip reason for additional subsystems

The winning swarm proposal (p-00000018) suggested 5 new Go files for fault tolerance. Those are out of scope for this fix — this plan addresses only the observed operational failures from the swarm test.

---

## Decisions Made

- Permission fix: bypassPermissions + --allowedTools gate (not settings.json pre-approval) — keeps session isolation, no config sprawl
- Log fix: Bus API as SSOT for session participation (not log scraping) — more reliable
- Version strategy: bump to 0.9.5 — patch-level fix

---

## Status
**Currently in Phase 1** — implementing critical fixes
