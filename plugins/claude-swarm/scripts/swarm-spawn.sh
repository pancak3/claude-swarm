#!/usr/bin/env bash
# swarm-spawn.sh — spawn a single Claude swarm session
# Usage: swarm-spawn.sh <session-id> <total-sessions> <perspective> <task-description> <bus-mcp-config>
set -euo pipefail

SESSION_ID="${1:?session-id required}"
SWARM_SIZE="${2:?total-sessions required}"
PERSPECTIVE="${3:?perspective required}"
TASK_DESCRIPTION="${4:?task-description required}"
BUS_MCP_CONFIG="${5:?bus-mcp-config-path required}"

# P0.2: Confine scratch files to the run directory.
export TMPDIR="${RUN_DIR:-/tmp}/tmp"
mkdir -p "${TMPDIR}"

# P1.2: Create session-specific output directory for isolated work.
# Prefer RUN_DIR from orchestrator; fall back to sessions/ relative to cwd.
if [ -n "${RUN_DIR:-}" ]; then
  SESSION_OUTPUT_DIR="${RUN_DIR}/sessions/${SESSION_ID}/output"
else
  SESSION_OUTPUT_DIR="sessions/${SESSION_ID}/output"
fi
mkdir -p "${SESSION_OUTPUT_DIR}"
export SESSION_OUTPUT_DIR

# P1.3: Set test spec file path if the orchestrator created one.
if [ -n "${RUN_DIR:-}" ] && [ -f "${RUN_DIR}/test_spec.md" ]; then
  cp "${RUN_DIR}/test_spec.md" "${SESSION_OUTPUT_DIR}/test_spec.md"
  chmod 444 "${SESSION_OUTPUT_DIR}/test_spec.md"
  export TEST_SPEC_FILE="${SESSION_OUTPUT_DIR}/test_spec.md"
fi


# Route through API proxy if available (per-session temperature etc.).
if [ -n "${SWARM_PROXY_PORT:-}" ]; then
  export ANTHROPIC_BASE_URL="http://127.0.0.1:${SWARM_PROXY_PORT}"
fi

# Default: max effort for deep parliamentary deliberation.
# Override via SWARM_MODEL, SWARM_EFFORT, SWARM_SETTINGS env vars.
SWARM_MODEL="${SWARM_MODEL:-}"
SWARM_EFFORT="${SWARM_EFFORT:-max}"
SWARM_SETTINGS="${SWARM_SETTINGS:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/../templates/swarm-prompt.md"

# Build the system prompt by substituting template variables.
# Use Python with a quoted heredoc to avoid shell interpolation entirely.
# Arguments are passed as separate argv entries, safe from special characters.
SYSTEM_PROMPT=$(python3 - "$TEMPLATE" "$SESSION_ID" "$SWARM_SIZE" "$PERSPECTIVE" "$TASK_DESCRIPTION" << 'PYEOF'
import sys
template_path, session_id, swarm_size, perspective, task_desc = sys.argv[1:6]
with open(template_path) as f:
    t = f.read()
t = t.replace('{SESSION_ID}', session_id)
t = t.replace('{SWARM_SIZE}', swarm_size)
t = t.replace('{PERSPECTIVE}', perspective)
t = t.replace('{TASK_DESCRIPTION}', task_desc)
# P1.2: Inject session output directory for isolated writes.
import os
out_dir = os.environ.get('SESSION_OUTPUT_DIR', f'sessions/{session_id}/output')
t = t.replace('{SESSION_OUTPUT_DIR}', out_dir)
sys.stdout.write(t)
PYEOF
) || { echo "[swarm-spawn] ERROR: failed to generate system prompt" >&2; exit 1; }

# P2.2: Inject orchestrator context if provided.
CONTEXT_FILE="${RUN_DIR}/context.json"
if [ -f "${CONTEXT_FILE}" ]; then
  SYSTEM_PROMPT="${SYSTEM_PROMPT}"$'\n\n## Orchestrator Context\n```json\n'"$(cat "${CONTEXT_FILE}")"$'\n```'
fi

# Generate a valid UUID for the session.
SESSION_UUID=$(python3 -c "import uuid; print(uuid.uuid4())")

# Spawn Claude session in print (non-interactive) mode.
# Inherits model, effort, and settings from parent by default.
# Only passes flags explicitly set via SWARM_MODEL / SWARM_EFFORT / SWARM_SETTINGS.
EXTRA_ARGS=()
# P0.1: SWARM_MODE controls filesystem access.
#   analyze   → read-only: Bash,Read,Grep,Glob,Web*,MCP allowed; Write/Edit blocked
#   implement → full access: bypassPermissions, all tools
SWARM_MODE="${SWARM_MODE:-analyze}"
if [ "${SWARM_MODE}" = "analyze" ]; then
  # Allow Bash, Read, Grep, Glob, WebSearch, WebFetch, AND all swarm-bus MCP tools.
  # mcp__swarm-bus prefix auto-allows swarm_register, submit_proposal, cast_vote, etc.
  # Write/Edit/NotebookEdit NOT in this list -> blocked in non-interactive mode.
  # Use bypassPermissions: --allowedTools gates which tools are available,
  # and claude -p (non-interactive) denies any tool NOT in the allowed list.
  # Without bypassPermissions, MCP tools fail because there is no interactive prompt.
  EXTRA_ARGS+=(--permission-mode bypassPermissions)
  EXTRA_ARGS+=(--allowedTools "Bash,Read,Grep,Glob,WebSearch,WebFetch,mcp__swarm-bus")
elif [ "${SWARM_MODE}" = "implement" ]; then
	EXTRA_ARGS+=(--dangerously-skip-permissions)
	EXTRA_ARGS+=(--allow-dangerously-skip-permissions)
	EXTRA_ARGS+=(--permission-mode bypassPermissions)
fi
# In analyze mode: --allowedTools restricts to read-only + MCP tools.
# Write/Edit/NotebookEdit are NOT in the allowed list, so they remain blocked
# even with bypassPermissions.
if [ -n "${SWARM_MODEL}" ]; then
  EXTRA_ARGS+=(--model "${SWARM_MODEL}")
fi
if [ -n "${SWARM_EFFORT}" ]; then
  EXTRA_ARGS+=(--effort "${SWARM_EFFORT}")
fi
if [ -n "${SWARM_SETTINGS}" ]; then
  EXTRA_ARGS+=(--settings "${SWARM_SETTINGS}")
fi
# claude -p reopens stdout/stderr to internal task files, so shell-level
# redirects are bypassed. Session contributions are captured by the bus API
# (proposals, critiques, votes) — the orchestrator reads bus data for
# synthesis. We emit a completion marker for the session log after claude exits.
#
# Run in background so we can trap signals and forward them to the child.
claude -p "Begin parliamentary deliberation. Register with the swarm bus (session_id=${SESSION_ID}, perspective=${PERSPECTIVE})." \
  --append-system-prompt "${SYSTEM_PROMPT}" \
  --mcp-config "${BUS_MCP_CONFIG}" \
  "${EXTRA_ARGS[@]}" \
  --session-id "${SESSION_UUID}" \
  < /dev/null > /dev/null 2>&1 &
CLAUDE_PID=$!

# Forward signals to claude child so it's not orphaned when spawn.sh is killed.
_cleanup_child() {
  # Write a marker so the orchestrator knows this session was killed.
  echo "[swarm-spawn] ${SESSION_ID}: killed (signal received before completion)" >&2
  if [ -n "${CLAUDE_PID:-}" ] && kill -0 "${CLAUDE_PID}" 2>/dev/null; then
    kill -TERM "${CLAUDE_PID}" 2>/dev/null || true
    sleep 2
    kill -KILL "${CLAUDE_PID}" 2>/dev/null || true
  fi
  exit 143  # 128 + SIGTERM
}
trap _cleanup_child INT TERM

wait "${CLAUDE_PID}" 2>/dev/null
CLAUDE_EXIT_CODE=$?
trap - INT TERM  # child exited cleanly, remove trap

echo "[swarm-spawn] ${SESSION_ID}: completed with exit code ${CLAUDE_EXIT_CODE}"
