# Claude Swarm

Fractal multi-session swarm for Claude Code. Replaces single-session execution with parliamentary deliberation across independent Claude CLI sessions.

## Quick Start

```bash
# Activate swarm mode (all tasks from now on)
/swarm-init

# Execute a single task via swarm
/swarm-init "Add OAuth2 authentication to the API"

# Open a question for debate
/swarm-debate "PostgreSQL vs MongoDB for this use case?"

# View/modify config
/swarm-config
```

## How It Works

```
User Task
    │
    ▼
3 Orchestrator Sessions (triumvirate) — debate the plan
    │
    ▼
N Worker Sessions — parliamentary deliberation
    │  Round 1: PROPOSE
    │  Round 2: CRITIQUE (+ fatal flaw elimination)
    │  Round 3: REBUTTAL
    │  Round 4: VOTE (instant-runoff)
    │  Round 5: EXECUTE
    │
    ▼
Sub-tasks → recursive sub-swarms
    │
    ▼
3 Synthesis Sessions — final consensus
    │
    ▼
User receives consensus answer
```

## Configuration

Edit `config/swarm.yaml`:

```yaml
max_sessions: 128        # Maximum concurrent sessions
orchestrator_count: 3    # Triumvirate size
depth_limit: 5           # Maximum recursion depth
round_timeouts:
  propose: 60s
  critique: 90s
  rebuttal: 60s
  vote: 30s
model: claude-sonnet-4-6
```

Or use `/swarm-config set <key> <value>`.

## Architecture

See `docs/superpowers/specs/2026-06-09-claude-swarm-design.md` for the full design specification.

## License

MIT
