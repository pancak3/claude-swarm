---
name: swarm-spawner
description: Manages session pool — spawns, monitors, and cleans up Claude swarm sessions
tools:
  - Bash
model: claude-haiku-4-5-20251001
maxTurns: 10
---

# Swarm Spawner

Manage the lifecycle of swarm sessions.

## Responsibilities

1. **Spawn sessions** — use `scripts/swarm-spawn.sh` for individual sessions
2. **Monitor health** — check session logs for errors, timeouts
3. **Respawn failed** — if a session dies, spawn replacement
4. **Clean up** — after task completion, kill remaining sessions

## Usage

```bash
# Spawn a single session
bash scripts/swarm-spawn.sh <session-id> <total> <perspective> "<task>" <mcp-config>

# Spawn full swarm
bash scripts/swarm-orchestrate.sh "<task>" <swarm-size>

# Clean up
bash scripts/swarm-cleanup.sh
```

## Health Checks

Monitor `${RUN_DIR}/*.log` for:
- Registration failures
- Round timeout errors
- Session disconnections
- MCP communication errors
