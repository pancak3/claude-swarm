#!/usr/bin/env bash
# swarm-orchestrate.sh — orchestrate a full parliamentary swarm cycle
# Usage: swarm-orchestrate.sh <task-description> [swarm-size] [--summarize] [--phase-sizes K=V,...] [--max-tokens N] [--port N] [--resume RUN_ID]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"
# Use project working directory for state isolation — each project gets its own .swarm-state.
# This prevents cross-project bus pollution when multiple repos use the plugin simultaneously.
PROJECT_DIR="${SWARM_PROJECT_DIR:-$(pwd)}"
STATE_DIR="${PROJECT_DIR}/.swarm-state"

# ── Option parsing (before positional args) ──────────────────────────────
SUMMARIZE=0
PHASE_SIZES=""
MAX_TOKENS=""
BUS_PORT=""
RESUME_ID=""
SWARM_MODE="${SWARM_MODE:-analyze}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--summarize) SUMMARIZE=1; shift ;;
		--phase-sizes) PHASE_SIZES="$2"; shift 2 ;;
		--max-tokens) MAX_TOKENS="$2"; shift 2 ;;
		--port) BUS_PORT="$2"; shift 2 ;;
		--resume) RESUME_ID="$2"; shift 2 ;;
		--implement) SWARM_MODE="implement"; shift ;;
		--read-only) SWARM_MODE="analyze"; shift ;;
		*) break ;;
	esac
done

TASK_DESCRIPTION="${1:?task-description required}"
SWARM_SIZE="${2:-8}"
MAX_PARALLEL="${SWARM_MAX_PARALLEL:-128}"

# P2.1: Triviality gate — size 0 means skip the swarm.
if [ "${SWARM_SIZE}" = "0" ]; then
  echo "[swarm] Task assessed as trivial — skipping swarm."
  echo "[swarm] Use --force-swarm in /swarm-init or pass a size > 0 directly."
  exit 0
fi

RUN_ID="${RESUME_ID:-swarm-$(date +%Y%m%d-%H%M%S)}"
RUN_DIR="${STATE_DIR}/${RUN_ID}"
mkdir -p "${RUN_DIR}"

# Helper: emit a timestamped event to stdout.
event() {
  echo "[$(date +%H:%M:%S)] [swarm] $*"
}

# ── Token budget tracking ────────────────────────────────────────────────
TOKEN_ESTIMATE=0
TOKEN_BUDGET_HIT=false
track_tokens() {
  local phase="$1" phase_sessions="$2" avg_lines="$3"
  local estimate=$(( phase_sessions * avg_lines ))
  TOKEN_ESTIMATE=$(( TOKEN_ESTIMATE + estimate ))
  event "Token budget: phase=${phase} +${estimate} (${phase_sessions} sessions × ~${avg_lines} lines) = ~${TOKEN_ESTIMATE} total"
  if [ -n "${MAX_TOKENS}" ]; then
    if [ "${TOKEN_ESTIMATE}" -gt "${MAX_TOKENS}" ]; then
      event "WARNING: Token budget exceeded (${TOKEN_ESTIMATE} > ${MAX_TOKENS}). Budget exhausted."
      TOKEN_BUDGET_HIT=true
    elif [ "$(( MAX_TOKENS - TOKEN_ESTIMATE ))" -lt "$(( MAX_TOKENS / 4 ))" ]; then
      event "WARNING: Approaching token budget (${TOKEN_ESTIMATE}/${MAX_TOKENS}). Remaining: $(( MAX_TOKENS - TOKEN_ESTIMATE ))"
    fi
  fi
}

# ── Checkpoint save/load ─────────────────────────────────────────────────
write_checkpoint() {
  local phase="$1" cp_file="${RUN_DIR}/checkpoint.json"
  cat > "${cp_file}" <<- EOF
	{
	  "run_id": "${RUN_ID}",
	  "phase": "${phase}",
	  "task": $(echo "${TASK_DESCRIPTION}" | jq -Rs '.'),
	  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
	  "swarm_size": ${SWARM_SIZE}
	}
	EOF
  event "Checkpoint: phase=${phase} → ${cp_file}"
}

read_checkpoint() {
  local cp_file="${RUN_DIR}/checkpoint.json"
  if [ -f "${cp_file}" ]; then
    local phase
    phase=$(jq -r '.phase // "none"' "${cp_file}" 2>/dev/null || echo "none")
    echo "${phase}"
  else
    echo "none"
  fi
}

event "Starting swarm run ${RUN_ID}"
event "Task: ${TASK_DESCRIPTION}"
event "Swarm size: ${SWARM_SIZE}"
event "Swarm mode: ${SWARM_MODE}"
event "Run dir: ${RUN_DIR}"

# ── Resume: skip if checkpoint says work is done ─────────────────────────
if [ -n "${RESUME_ID}" ]; then
  CP_PHASE=$(read_checkpoint)
  event "Resume mode: checkpoint phase=${CP_PHASE}"
  if [ "${CP_PHASE}" = "SYNTHESIS" ] || [ "${CP_PHASE}" = "CLOSED" ]; then
    event "Swarm run ${RUN_ID} already completed (phase=${CP_PHASE}). Skipping."
    exit 0
  fi
fi

# ── Bus port cleanup: check for stale PID file ────────────────────────────
BUS_PID_FILE="${RUN_DIR}/bus.pid"
if [ -f "${BUS_PID_FILE}" ]; then
  OLD_PID=$(cat "${BUS_PID_FILE}" 2>/dev/null)
  if [ -n "${OLD_PID}" ] && kill -0 "${OLD_PID}" 2>/dev/null; then
    # Check it's actually a swarm-bus process
    if ps -p "${OLD_PID}" -o comm= 2>/dev/null | grep -q "swarm-bus"; then
      event "WARNING: Stale bus process found (PID ${OLD_PID}), killing..."
      kill "${OLD_PID}" 2>/dev/null || true
      sleep 1
      kill -9 "${OLD_PID}" 2>/dev/null || true
      event "Stale bus process terminated."
    fi
  fi
	# 3. Kill API proxy (sessions and bus are already down).
	if [ -n "${PROXY_PID:-}" ]; then
	  kill "${PROXY_PID}" 2>/dev/null || true
	  wait "${PROXY_PID}" 2>/dev/null || true
	  event "API proxy stopped."
	fi
  rm -f "${BUS_PID_FILE}"
fi

# ── Export default phase timeouts so the bus picks them up ──────────────────
# These can be overridden by --phase-sizes or by setting the env var directly.
export SWARM_TIMEOUT_REGISTER="${SWARM_TIMEOUT_REGISTER:-300s}"
export SWARM_TIMEOUT_PROPOSE="${SWARM_TIMEOUT_PROPOSE:-150s}"
export SWARM_TIMEOUT_VOTE="${SWARM_TIMEOUT_VOTE:-300s}"
export SWARM_TIMEOUT_EXECUTE="${SWARM_TIMEOUT_EXECUTE:-120s}"

# ── Phase sizes: convert --phase-sizes to SWARM_TIMEOUT_* env vars ───────
if [ -n "${PHASE_SIZES}" ]; then
  event "Phase sizes: ${PHASE_SIZES}"
  IFS=',' read -ra PAIRS <<< "${PHASE_SIZES}"
  for pair in "${PAIRS[@]}"; do
    key="${pair%%=*}"
    val="${pair#*=}"
    case "${key}" in
      REGISTER) export SWARM_TIMEOUT_REGISTER="${val}s" ;;
      PROPOSE)  export SWARM_TIMEOUT_PROPOSE="${val}s"  ;;
      CRITIQUE) export SWARM_TIMEOUT_CRITIQUE="${val}s" ;;
      REBUTTAL) export SWARM_TIMEOUT_REBUTTAL="${val}s" ;;
      VOTE)     export SWARM_TIMEOUT_VOTE="${val}s"     ;;
      EXECUTE)  export SWARM_TIMEOUT_EXECUTE="${val}s"  ;;
      *) event "WARNING: Unknown phase key '${key}' in --phase-sizes" ;;
    esac
  done
fi

# ── Start API proxy for per-session parameter injection (temperature etc.) ──
PROXY_BIN="${SCRIPT_DIR}/swarm-proxy.py"
PROXY_CONFIG="${RUN_DIR}/proxy-config.json"
PROXY_LOG="${RUN_DIR}/proxy.log"
PROXY_PID=""

if [ -f "${PROXY_BIN}" ] && command -v python3 &>/dev/null; then
  # ── Role pool: select parameter pairs based on query type ────────────────
  # Each role gets 3 sessions (correctness, simplicity, performance).
  # Oracle (temp=0,top_p=0) and Chaos (temp=2,top_p=1) are always included.
  # Select additional roles via SWARM_QUERY_TYPE or SWARM_ROLES env var.
  ROLES_FILE="${PLUGIN_ROOT}/config/roles.json"
  python3 -c "
import json, os, sys

# Load role definitions
with open('${ROLES_FILE}') as f:
    pool = json.load(f)

roles_data = pool['roles']
perspectives = pool['meta']['perspectives']  # [correctness, simplicity, performance]
sessions_per = pool['meta']['sessions_per_role']  # 3

# Determine which roles to use.
selected_names = list(pool['meta']['mandatory'])  # ['oracle', 'chaos']

query_type = os.environ.get('SWARM_QUERY_TYPE', '')
custom_roles = os.environ.get('SWARM_ROLES', '')

if custom_roles:
    selected_names += [r.strip() for r in custom_roles.split(',') if r.strip()]
elif query_type and query_type in pool.get('query_types', {}):
    selected_names += pool['query_types'][query_type]['roles']
else:
    # Default: balanced set
    selected_names += ['judge', 'architect', 'innovator']

# Deduplicate while preserving order.
seen = set()
ordered = []
for r in selected_names:
    if r not in seen and r in roles_data:
        ordered.append(r)
        seen.add(r)
selected_names = ordered

# Generate sessions: each role gets 3 sessions, one per perspective.
config = {}
total = 0
for role_name in selected_names:
    rd = roles_data[role_name]
    for pi, persp in enumerate(perspectives):
        total += 1
        sid = f's{total}'
        config[sid] = {
            'temperature': rd['temp'],
            'top_p': rd['top_p'],
            'role': role_name,
            'perspective': persp
        }

with open('${PROXY_CONFIG}', 'w') as f:
    json.dump(config, f, indent=2)

# Emit count for orchestrator.
print(f'{total}')
sys.stderr.write(f'Roles: {selected_names} -> {total} sessions\\n')
" 2>/dev/null > "${RUN_DIR}/swarm-size-from-roles"

  ROLE_SESSION_COUNT=$(cat "${RUN_DIR}/swarm-size-from-roles" 2>/dev/null || echo "")
  if [ -n "${ROLE_SESSION_COUNT}" ] && [ "${ROLE_SESSION_COUNT}" -gt 0 ] 2>/dev/null; then
    # Only override swarm size when SWARM_AUTO_SIZE=1 (opt-in).
    if [ "${SWARM_AUTO_SIZE:-0}" = "1" ]; then
      if [ "${SWARM_SIZE}" -lt "${ROLE_SESSION_COUNT}" ] 2>/dev/null; then
        event "Role pool overrides swarm size: ${SWARM_SIZE} -> ${ROLE_SESSION_COUNT}"
        SWARM_SIZE="${ROLE_SESSION_COUNT}"
      fi
    else
      event "Role pool: ${ROLE_SESSION_COUNT} sessions from roles (using user size: ${SWARM_SIZE})"
    fi
  fi

  # Start proxy on a random port.
  python3 "${PROXY_BIN}" > "${PROXY_LOG}" 2>&1 &
  PROXY_PID=$!
  # Wait for proxy to print its port (longer timeout, check file non-empty).
  PROXY_PORT=""
  for i in $(seq 1 20); do
    if [ -s "${PROXY_LOG}" ]; then
      PROXY_PORT=$(grep -oE "SWARM_PROXY_PORT=[0-9]+" "${PROXY_LOG}" 2>/dev/null | head -1 | cut -d= -f2 || true)
      [ -n "${PROXY_PORT}" ] && break
    fi
    sleep 0.5
  done
  if [ -n "${PROXY_PORT}" ]; then
    event "API proxy started on port ${PROXY_PORT} (PID ${PROXY_PID})"
    export SWARM_PROXY_PORT="${PROXY_PORT}"
  else
    event "WARNING: API proxy failed to start within 10s — sessions use direct API."
    kill "${PROXY_PID}" 2>/dev/null || true
    wait "${PROXY_PID}" 2>/dev/null || true
    PROXY_PID=""
  fi
fi

# ── Build & start Swarm Bus ──────────────────────────────────────────────
BUS_BIN="${PLUGIN_ROOT}/swarm-bus/swarm-bus"
if [ ! -x "${BUS_BIN}" ] || [ "${PLUGIN_ROOT}/swarm-bus/main.go" -nt "${BUS_BIN}" ]; then
  event "Building swarm-bus..."
  (cd "${PLUGIN_ROOT}/swarm-bus" && go build -o swarm-bus .)
fi

event "Starting Swarm Bus..."
BUS_LOG="${RUN_DIR}/bus.log"
CHECKPOINT_FILE="${RUN_DIR}/checkpoint.json"

SWARM_CHECKPOINT_FILE="${CHECKPOINT_FILE}" \
	SWARM_SIZE="${SWARM_SIZE}" \
SWARM_TASK_ID="${RUN_ID}" \
SWARM_TASK_DESCRIPTION="${TASK_DESCRIPTION}" \
SWARM_BUS_PORT="${BUS_PORT}" \
SWARM_BUS_ADDR="${SWARM_BUS_ADDR:-}" \
  "${BUS_BIN}" > "${BUS_LOG}" 2>&1 &
BUS_PID=$!

# Wait for bus to print its port.
FOUND_PORT=""
for i in $(seq 1 30); do
  if [ -f "${BUS_LOG}" ]; then
    FOUND_PORT=$(grep -oE "SWARM_BUS_PORT=[0-9]+" "${BUS_LOG}" 2>/dev/null | head -1 | cut -d= -f2 || true)
    if [ -n "${FOUND_PORT}" ]; then
      break
    fi
  fi
  sleep 0.5
done

if [ -z "${FOUND_PORT}" ]; then
  event "ERROR: Swarm Bus failed to start within 15s"
  kill "${BUS_PID}" 2>/dev/null || true
  exit 1
fi

# Write PID file so future runs can detect and clean up stale processes.
echo "${BUS_PID}" > "${BUS_PID_FILE}"

# Use explicit port if provided, otherwise detected port
if [ -z "${BUS_PORT}" ]; then
  BUS_PORT="${FOUND_PORT}"
fi
event "Swarm Bus started on port ${BUS_PORT} (PID ${BUS_PID})"

# Progress is shown in the FINAL SYNTHESIS block after sessions complete.
# No polling or background processes — just wait for sessions to finish.

# Create temporary MCP config pointing to the shared bus.
BUS_MCP_CONFIG="${RUN_DIR}/mcp-config.json"
cat > "${BUS_MCP_CONFIG}" << EOF
{
  "mcpServers": {
    "swarm-bus": {
      "type": "http",
      "url": "http://localhost:${BUS_PORT}/mcp"
    }
  }
}
EOF


# Cleanup function to kill ALL background processes on exit.
# This ensures no orphaned processes leak across projects.
cleanup() {
  # 1. Kill spawned session processes first (they depend on the bus).
  if [ ${#SESSION_PIDS[@]} -gt 0 ]; then
    event "Stopping ${#SESSION_PIDS[@]} spawned session(s)..."
    for spid in "${SESSION_PIDS[@]}"; do
      kill "${spid}" 2>/dev/null || true
    done
    wait "${SESSION_PIDS[@]}" 2>/dev/null || true
  fi
  # 2. Kill bus last (sessions depend on it being up).
  if [ -n "${BUS_PID:-}" ]; then
    event "Shutting down Swarm Bus (PID ${BUS_PID})..."
    kill "${BUS_PID}" 2>/dev/null || true
    wait "${BUS_PID}" 2>/dev/null || true
    event "Swarm Bus stopped."
  fi
	# 3. Kill API proxy (sessions and bus are already down).
	if [ -n "${PROXY_PID:-}" ]; then
	  kill "${PROXY_PID}" 2>/dev/null || true
	  wait "${PROXY_PID}" 2>/dev/null || true
	  event "API proxy stopped."
	fi
  rm -f "${BUS_PID_FILE}"

  # P0.2: Quarantine stray backup files left by worker sessions.
  # Sessions that bypass the read-only guard may leave *.bak_s* / *_backup
  # / *_final files in the project root. Move them into .swarm-state/artifacts/.
  local artifact_dir="${STATE_DIR}/artifacts"
  local stray_count=0
  for pattern in '*.bak_s[0-9]*' '*_backup' '*_s[0-9]*_final' '*_s[0-9]*_backup'; do
    for f in ${pattern}; do
      if [ -f "${f}" ]; then
        mkdir -p "${artifact_dir}"
        local dest="${artifact_dir}/$(basename "${f}")"
        mv "${f}" "${dest}" 2>/dev/null && stray_count=$((stray_count + 1))
      fi
    done
  done
  if [ "${stray_count}" -gt 0 ]; then
    event "Quarantined ${stray_count} stray backup files → ${artifact_dir}/"
  fi
}
trap cleanup EXIT INT TERM

# Perspectives rotate through sessions.
PERSPECTIVES=("correctness" "simplicity" "performance" "security")
export RUN_DIR  # so swarm-spawn.sh can reference it for session output dirs
export SWARM_MODE  # P0.1: read-only enforcement for analyze mode

# Create session output directories for inter-session merge isolation.
for ((i=1; i<=SWARM_SIZE; i++)); do
  mkdir -p "${RUN_DIR}/sessions/s${i}/output"
done

# ── TEST_SPEC: if provided, write test stubs before spawning sessions ──────
TEST_SPEC="${SWARM_TEST_SPEC:-}"
if [ -n "${TEST_SPEC}" ]; then
  event "Test spec mode enabled. Writing test stubs..."
  TEST_DIR="${RUN_DIR}/tests"
  mkdir -p "${TEST_DIR}"
  echo "${TEST_SPEC}" > "${TEST_DIR}/test_spec.json"
  # Unpack test files from test_spec.json (format: {files: [{path, content}]})
  if echo "${TEST_SPEC}" | jq -e '.files' >/dev/null 2>&1; then
    echo "${TEST_SPEC}" | jq -c '.files[]' 2>/dev/null | while read -r tf; do
      tf_path=$(echo "${tf}" | jq -r '.path')
      tf_content=$(echo "${tf}" | jq -r '.content')
      mkdir -p "$(dirname "${TEST_DIR}/${tf_path}")"
      printf '%s' "${tf_content}" > "${TEST_DIR}/${tf_path}"
      event "  Test stub created: ${TEST_DIR}/${tf_path}"
    done
  fi
  event "Test files are immutable during EXECUTE: ${TEST_DIR}/"
	# Write summary test_spec.md for sessions.
	echo "Implementation must pass tests in ${TEST_DIR}/" > "${RUN_DIR}/test_spec.md"
	if [ -d "${TEST_DIR}" ]; then
	  find "${TEST_DIR}" -type f | head -20 | while read tf; do
	    echo "  - ${tf}" >> "${RUN_DIR}/test_spec.md"
	  done
	fi
	# Copy test spec to each session output dir and make immutable.
	for ((si=1; si<=SWARM_SIZE; si++)); do
	  tdir="${RUN_DIR}/sessions/s${si}/output/tests"
	  mkdir -p "${tdir}"
	  if [ -d "${TEST_DIR}" ]; then
	    cp -r "${TEST_DIR}/"* "${tdir}/" 2>/dev/null || true
	  fi
	  find "${tdir}" -type f -exec chmod 444 {} \; 2>/dev/null || true
	done
	event "Test files made immutable across all session output dirs"
fi


# Launch sessions in parallel with concurrency control.
SESSION_PIDS=()
for ((i=1; i<=SWARM_SIZE; i++)); do
  SESSION_ID="s${i}"
  PERSPECTIVE="${PERSPECTIVES[$(( (i-1) % ${#PERSPECTIVES[@]} ))]}"

  event "Spawning session ${SESSION_ID} (${PERSPECTIVE})..."

  "${SCRIPT_DIR}/swarm-spawn.sh" \
    "${SESSION_ID}" \
    "${SWARM_SIZE}" \
    "${PERSPECTIVE}" \
    "${TASK_DESCRIPTION}" \
    "${BUS_MCP_CONFIG}" \
    > "${RUN_DIR}/${SESSION_ID}.log" 2>&1 &

  SESSION_PIDS+=($!)

  # Throttle at MAX_PARALLEL: count actual background jobs.
  while true; do
    JOBS=$(jobs -rp | wc -l)
    [ "${JOBS}" -lt "${MAX_PARALLEL}" ] && break
    event "${JOBS} active jobs (max ${MAX_PARALLEL}), waiting for slot..."
    wait -n 2>/dev/null || true
  done
done

event "All ${SWARM_SIZE} sessions spawned. Waiting for registration..."
track_tokens "spawn" "${SWARM_SIZE}" 5

# Wait for a quorum of sessions to register before advancing.
# Self-vote is blocked, so ≥2 distinct proposers are required for a vote.
# Quorum: max(2, 60% of SWARM_SIZE), or all spawned — whichever comes first.
# Timeout from SWARM_TIMEOUT_REGISTER (default 90s).
REG_TIMEOUT="${SWARM_TIMEOUT_REGISTER:-90s}"
REG_TIMEOUT_SEC="${REG_TIMEOUT%s}"
REG_TRIES=$(( REG_TIMEOUT_SEC * 2 ))  # 0.5s per iteration
REQUIRED_QUORUM=$(( SWARM_SIZE * 60 / 100 ))
[ "${REQUIRED_QUORUM}" -lt 2 ] && REQUIRED_QUORUM=2
[ "${REQUIRED_QUORUM}" -gt "${SWARM_SIZE}" ] && REQUIRED_QUORUM="${SWARM_SIZE}"
event "Waiting up to ${REG_TIMEOUT_SEC}s for quorum (≥${REQUIRED_QUORUM}/${SWARM_SIZE})..."

registered=0
stable_count=0
last_registered=0
for try in $(seq 1 ${REG_TRIES}); do
  registered=$(curl -sf --max-time 2 "http://127.0.0.1:${BUS_PORT}/status" 2>/dev/null | jq -r '.active_sessions // 0' 2>/dev/null || echo "0")

  # Count stable polls (count hasn't changed for 2 consecutive checks).
  if [ "${registered}" -eq "${last_registered}" ]; then
    stable_count=$((stable_count + 1))
  else
    stable_count=0
  fi
  last_registered="${registered}"

  # Advance when quorum reached AND count is stable (no more sessions joining).
  if [ "${registered}" -ge "${REQUIRED_QUORUM}" ] && [ "${stable_count}" -ge 2 ]; then
    event "${registered}/${SWARM_SIZE} session(s) registered (quorum met, stable)."
    break
  fi
  # Also advance when ALL expected sessions have registered.
  if [ "${registered}" -ge "${SWARM_SIZE}" ]; then
    event "All ${SWARM_SIZE} session(s) registered."
    break
  fi

  sleep 0.5
done

if [ "${registered}" -lt 2 ]; then
  event "WARNING: Only ${registered} session(s) registered (need ≥2 for voting)."
fi

if [ "${registered}" -lt 2 ]; then
  write_checkpoint "REGISTER_ABORT"
  event "ERROR: Only ${registered} session(s) registered within ${REG_TIMEOUT_SEC}s (need ≥2). Aborting swarm."
  cleanup
  exit 1
fi

# P2#1: Checkpoint after REGISTER phase
write_checkpoint "REGISTER"
track_tokens "register" "${SWARM_SIZE}" "5"

# H1 fix: poll bus for completion instead of waiting on all PIDs forever.
# Some sessions never exit (they just sit idle after bus closes).
# Timeout from env, default 30 minutes.
WAIT_TIMEOUT="${SWARM_WAIT_TIMEOUT:-1800}"
event "Waiting for sessions to complete (polling bus, timeout=${WAIT_TIMEOUT}s)..."

WAIT_DEADLINE=$(( $(date +%s) + WAIT_TIMEOUT ))
while true; do
  # Check if bus reports CLOSED.
  BUS_ROUND=$(curl -sf --max-time 2 "http://127.0.0.1:${BUS_PORT}/status" 2>/dev/null | jq -r '.round // ""' 2>/dev/null || echo "")
  if [ "${BUS_ROUND}" = "CLOSED" ]; then
    event "Bus reports CLOSED — sessions complete."
    break
  fi

  # Check timeout.
  if [ "$(date +%s)" -ge "${WAIT_DEADLINE}" ]; then
    event "WARNING: Wait timeout (${WAIT_TIMEOUT}s) reached. Forcing advance."
    break
  fi

  # Count alive PIDs.
  alive=0
  for spid in "${SESSION_PIDS[@]}"; do
    kill -0 "${spid}" 2>/dev/null && alive=$((alive + 1))
  done
  if [ "${alive}" -eq 0 ]; then
    event "All ${#SESSION_PIDS[@]} session PIDs exited."
    break
  fi

  sleep 5
done

# Force-kill any remaining sessions.
for spid in "${SESSION_PIDS[@]}"; do
  kill "${spid}" 2>/dev/null || true
done
wait "${SESSION_PIDS[@]}" 2>/dev/null || true

event "All sessions finished."
write_checkpoint "SESSIONS_DONE"
track_tokens "sessions" "${#SESSION_PIDS[@]}" "50"

# P2#1: Checkpoint after PROPOSE/VOTE phases
write_checkpoint "PROPOSE"
track_tokens "propose" "${SWARM_SIZE}" "80"
track_tokens "vote" "${SWARM_SIZE}" "30"

# ── Session failure detection ─────────────────────────────────────────
# Check if sessions silently failed: 0 proposals AND 0 votes = no work done.
FAILURE_DETECTED=0
BUS_STATUS=$(curl -sf --max-time 3 "http://127.0.0.1:${BUS_PORT}/status" 2>/dev/null || echo "{}")
FINAL_PROPS=$(echo "${BUS_STATUS}" | jq -r '.proposals_submitted // 0' 2>/dev/null)
FINAL_VOTES=$(echo "${BUS_STATUS}" | jq -r '.votes_cast // 0' 2>/dev/null)
if [ "${FINAL_PROPS}" -eq 0 ] && [ "${FINAL_VOTES}" -eq 0 ]; then
  FAILURE_DETECTED=1
  event "ERROR: 0 proposals and 0 votes — all sessions appear to have failed silently."
  event "ERROR: Check session logs in ${RUN_DIR}/ for details."
  # Find first session log with actual content.
  FIRST_FAIL_LOG=""
  for ((i=1; i<=SWARM_SIZE; i++)); do
    slog="${RUN_DIR}/s${i}.log"
    if [ -f "${slog}" ] && [ -s "${slog}" ]; then
      FIRST_FAIL_LOG="${slog}"
      break
    fi
  done
  if [ -n "${FIRST_FAIL_LOG}" ]; then
    event "ERROR: First session log with content: ${FIRST_FAIL_LOG}"
    event "ERROR: Latest lines from first failing session:"
    tail -5 "${FIRST_FAIL_LOG}" 2>/dev/null | while read -r line; do event "  | ${line}"; done
  else
    event "ERROR: No session logs found with any content."
  fi
fi

# ── Contract.json: persist shared naming registry from bus ──────────────
CONTRACT_FILE="${RUN_DIR}/contract.json"
if [ "${FAILURE_DETECTED}" = "0" ]; then
  CONTRACT_DATA=$(curl -sf --max-time 3 "http://127.0.0.1:${BUS_PORT}/contract" 2>/dev/null || echo '{"entries":[]}')
  echo "${CONTRACT_DATA}" > "${CONTRACT_FILE}"
  CONTRACT_COUNT=$(echo "${CONTRACT_DATA}" | jq '.entries | length // 0' 2>/dev/null)
  if [ "${CONTRACT_COUNT}" -gt 0 ]; then
    event "Contract registry: ${CONTRACT_COUNT} module name(s) registered."
  fi
fi

# ── Meta-synthesizer (lightweight text analysis, no LLM) ─────────────────
meta_synthesize() {
  local rundir="$1" size="$2" port="$3"
  echo ""
  echo "╔═══════════════════════════════════════════════════════════════════╗"
  echo "║              META-SYNTHESIS (Summarized Findings)                 ║"
  echo "╚═══════════════════════════════════════════════════════════════════╝"
  echo ""

  # Collect key phrases from session conclusions.
  local all_words=""
  for ((i=1; i<=size; i++)); do
    local slog="${rundir}/s${i}.log"
    if [ -f "${slog}" ]; then
      # Extract the last substantial paragraph (same as session conclusions).
      local conclusion
      conclusion=$(awk -v RS='' 'length($0) >= 80 { last = $0 } END { if (last != "") { gsub(/^[[:space:]]+|[[:space:]]+$/, "", last); print last } }' "${slog}" 2>/dev/null)
      if [ -n "${conclusion}" ]; then
        all_words="${all_words}"$'\n'"${conclusion}"
      fi
    fi
  done

  # ── Key findings: frequency-count significant phrases (2‑4 word n-grams) ──
  echo "═══ Key Findings (from frequency analysis) ═══"
  if [ -n "${all_words}" ]; then
    local freq_report
    freq_report=$(echo "${all_words}" \
      | tr '[:space:]' '\n' \
      | tr '[:upper:]' '[:lower:]' \
      | sed 's/[^a-z0-9/-]//g' \
      | grep -E '^[a-z][a-z0-9/-]{2,}$' \
      | sort | uniq -c | sort -rn | head -20 2>/dev/null)
    if [ -n "${freq_report}" ]; then
      echo "  Top terms across all session conclusions:"
      echo "${freq_report}" | awk '{ printf "    %6s  %s\n", $1, $2 }'
    else
      echo "  (no significant terms extracted)"
    fi
  else
    echo "  (no session conclusions available)"
  fi
  echo ""

  # ── Consensus areas: terms appearing across multiple sessions ──
  echo "═══ Terms by Session Coverage ═══"
  local term_sessions=""
  if [ -n "${all_words}" ]; then
    term_sessions=$(echo "${all_words}" \
      | tr '[:upper:]' '[:lower:]' \
      | sed 's/[^a-z0-9 \n/-]//g' \
      | tr ' ' '\n' \
      | grep -E '^[a-z][a-z0-9/-]{3,}$' \
      | sort -u)
    echo "  (session-level term coverage — see individual conclusions)"
  fi
  echo ""
}

# ── Gini diversity helper ──────────────────────────────────────────────────
compute_gini() {
  local status_json="$1"
  # Extract vote counts from the first vote round.
  local vote_counts
  vote_counts=$(echo "${status_json}" | jq -r '.vote_rounds[0].candidate_votes // {}' 2>/dev/null || echo "{}")
  if [ "${vote_counts}" = "{}" ]; then
    echo "N/A"
    return
  fi
  # Compute Gini via Python for correct floating point.
  python3 -c "
import json, sys
data = json.loads('${vote_counts}')
vals = sorted(data.values())
n = len(vals)
if n <= 1 or sum(vals) == 0:
  print('1.0')
else:
  s = sum(vals)
  ws = sum((i+1)*v for i,v in enumerate(vals))
  g = (2*ws)/(n*s) - (n+1)/n
  if g < 0: g = 0
  print(f'{g:.3f}')
" 2>/dev/null || echo "N/A"
}

# ── Final synthesis: surface debate outcomes ──────────────────────────
synthesize_results() {
  local port="$1" rundir="$2" buslog="$3" size="$4" fail_detected="$5"
  local perspectives=("correctness" "simplicity" "performance" "security")

  # Wait for bus to reach CLOSED (max 15s).
  local round=""
  for try in $(seq 1 30); do
    round=$(curl -sf --max-time 2 "http://127.0.0.1:${port}/status" 2>/dev/null | jq -r '.round // ""' 2>/dev/null || echo "")
    [ "${round}" = "CLOSED" ] && break
    sleep 0.5
  done

  # ── Fetch results from /status (the single authoritative HTTP endpoint) ──
  local status_json
  status_json=$(curl -sf --max-time 3 "http://127.0.0.1:${port}/status" 2>/dev/null || echo "{}")

  local proposals_json vote_rounds_json
  proposals_json=$(echo "${status_json}" | jq '.proposals // []' 2>/dev/null || echo "[]")
  vote_rounds_json=$(echo "${status_json}" | jq '.vote_rounds // []' 2>/dev/null || echo "[]")

  # ── Derive winner: first from /status winner field, fallback to bus log ──
  local winner_id elim_list tally_rounds
  winner_id=$(echo "${status_json}" | jq -r '.winner // ""' 2>/dev/null || echo "")
  if [ -z "${winner_id}" ] || [ "${winner_id}" = "null" ]; then
    # grep the bus stderr log for "synthesis — winner: p-XXXX"
    winner_id=$(grep -oE "winner: [a-z0-9-]+" "${buslog}" 2>/dev/null | head -1 | sed "s/winner: //")
  fi

  # Count eliminated proposals (proposals where eliminated==true).
  elim_list=$(echo "${proposals_json}" | jq -r '[.[] | select(.eliminated) | .id] | join(", ")' 2>/dev/null || echo "")
  tally_rounds=$(echo "${vote_rounds_json}" | jq -r 'length // 0' 2>/dev/null || echo 0)

  local props votes active total
  props=$(echo "${status_json}" | jq -r '.proposals_submitted // 0' 2>/dev/null)
  votes=$(echo "${status_json}" | jq -r '.votes_cast // 0'         2>/dev/null)
  active=$(echo "${status_json}"| jq -r '.active_sessions // 0'    2>/dev/null)
  total=$(echo "${status_json}" | jq -r '.total_sessions // 0'     2>/dev/null)

  # ── Display header ─────────────────────────────────────────────────────
  echo ""
  echo "╔═══════════════════════════════════════════════════════════════════╗"
  echo "║                    SWARM FINAL SYNTHESIS                        ║"
  echo "╚═══════════════════════════════════════════════════════════════════╝"
  echo ""
  echo "  Run:   ${RUN_ID}"
  echo "  Task:  ${TASK_DESCRIPTION}"
  echo ""

  # ── Failure detection warning ──────────────────────────────────────────
  if [ "${fail_detected}" = "1" ]; then
    echo "  !! WARNING: All sessions appear to have failed silently."
    echo "  !! 0 proposals and 0 votes detected."
    echo "  !! Check session logs: ${rundir}/"
    FIRST_FAIL=""
    for ((i=1; i<=size; i++)); do
      sf="${rundir}/s${i}.log"
      if [ -f "${sf}" ] && [ -s "${sf}" ]; then
        FIRST_FAIL="${sf}"
        break
      fi
    done
    if [ -n "${FIRST_FAIL}" ]; then
      echo "  !! First failing session log: ${FIRST_FAIL}"
    fi
    echo ""
  fi

  # ── Winning proposal with content ──────────────────────────────────────
  echo "═══ Winning Proposal ═══"
  if [ -n "${winner_id}" ]; then
    echo "  ID: ${winner_id}"
    # Extract winning proposal from the proposals array.
    local winner_approach winner_arch winner_risks winner_conf winner_sub
    if echo "${proposals_json}" | jq -e '. | length > 0' >/dev/null 2>&1; then
      winner_approach=$(echo "${proposals_json}" | jq -r --arg wid "${winner_id}" '.[] | select(.id==$wid) | .approach // ""' 2>/dev/null)
      winner_arch=$(echo "${proposals_json}"     | jq -r --arg wid "${winner_id}" '.[] | select(.id==$wid) | .architecture // ""' 2>/dev/null)
      winner_risks=$(echo "${proposals_json}"    | jq -r --arg wid "${winner_id}" '.[] | select(.id==$wid) | .risks // [] | join("; ")' 2>/dev/null)
      winner_conf=$(echo "${proposals_json}"     | jq -r --arg wid "${winner_id}" '.[] | select(.id==$wid) | .confidence // ""' 2>/dev/null)
      winner_sub=$(echo "${proposals_json}"      | jq -r --arg wid "${winner_id}" '.[] | select(.id==$wid) | .estimated_subtasks // ""' 2>/dev/null)
      echo ""
      if [ -n "${winner_approach}" ]; then
        echo "  Approach:"
        echo "${winner_approach}" | fold -s -w 74 | awk '{print "    " $0}'
        echo ""
        echo "  Architecture:"
        echo "${winner_arch}" | fold -s -w 74 | awk '{print "    " $0}'
        echo ""
        echo "  Risks: ${winner_risks}"
        echo "  Confidence: ${winner_conf}  |  Estimated subtasks: ${winner_sub}"
        if [ -n "${winner_risks}" ]; then
          echo ""
          echo "  ╔═══════════════════════════════════════════════════════╗"
          echo "  ║  VERIFY BEFORE APPLYING — winner self-flagged risks  ║"
          echo "  ║  Re-read the source to confirm every claim above.     ║"
          echo "  ╚═══════════════════════════════════════════════════════╝"
        fi
      else
        echo "  (proposal content not available via API)"
      fi
    fi
  else
    echo "  (no winner — vote tally did not produce a result)"
	    # H2: when votes==0, synthesize from proposals anyway.
	    if [ "${props}" -gt 0 ]; then
	      echo ""
	      echo "  Submissions (from ${props} proposal(s)):"
	      echo "${proposals_json}" | jq -r '.[] | "    [\\(.id)] \\(.session_id // \"?\"): \\(.approach // \"?\" | .[0:120])"' 2>/dev/null | head -20
	    fi
  fi
  echo ""

  # ── Vote tally ─────────────────────────────────────────────────────────
  echo "═══ Vote Tally Results ═══"
  echo "  Proposals submitted: ${props}"
  echo "  Votes cast:          ${votes}"
  echo "  Sessions:            ${active} active / ${total} total"

  # ── Cost estimate ─────────────────────────────────────────────────────────
  local total_in total_out
  total_in=$(echo "${status_json}" | jq -r '[.sessions[]?.tokens_in // 0] | add // 0' 2>/dev/null || echo 0)
  total_out=$(echo "${status_json}" | jq -r '[.sessions[]?.tokens_out // 0] | add // 0' 2>/dev/null || echo 0)
  if [ "${total_in}" != "0" ] || [ "${total_out}" != "0" ]; then
    local cost_in_per_m cost_out_per_m
    cost_in_per_m="${SWARM_COST_INPUT_PER_M:-0.435}"
    cost_out_per_m="${SWARM_COST_OUTPUT_PER_M:-0.87}"
    local total_cost
    total_cost=$(python3 -c "
tin = ${total_in} / 1_000_000.0 * ${cost_in_per_m}
tout = ${total_out} / 1_000_000.0 * ${cost_out_per_m}
total = tin + tout
if total >= 0.01:
    print(f'\${total:.4f}')
else:
    print(f'{total*100:.2f}¢')
" 2>/dev/null || echo '-')
    echo "  Tokens:              ${total_in} in / ${total_out} out"
    echo "  Est. cost (DSv4-Pro): ${total_cost}"
  fi

  if [ -n "${elim_list}" ] && [ "${elim_list}" != "none" ]; then
    echo "  Eliminated:          ${elim_list}"
  fi

  # P3.4: Vote diversity from bus API.
  local diversity_score degenerated
  diversity_score=$(echo "${status_json}" | jq -r '.diversity_score // ""' 2>/dev/null || echo "")
  degenerated=$(echo "${status_json}" | jq -r '.degenerate_vote // false' 2>/dev/null || echo "false")
  if [ -n "${diversity_score}" ] && [ "${diversity_score}" != "null" ] && [ "${votes}" -gt 0 ]; then
    echo "  Diversity (Gini):   ${diversity_score}"
    if [ "${degenerated}" = "true" ]; then
      echo "  (single surviving candidate — no diversity to measure)"
    elif awk "BEGIN { exit (${diversity_score} < 0.3 ? 0 : 1) }" 2>/dev/null; then
      echo "  !! WARNING: Low vote diversity (Gini=${diversity_score} < 0.3). Sessions may be voting without deliberation."
    fi
  fi

  # Per-round IRV breakdown.
  if [ "${tally_rounds}" -gt 0 ]; then
    for ((r=0; r<tally_rounds; r++)); do
      local prefix="  "
      local eliminated_this=""
      eliminated_this=$(echo "${vote_rounds_json}" | jq -r ".[${r}].eliminated // \"\"" 2>/dev/null)
      if [ -n "${eliminated_this}" ]; then
        echo "  ── Round $((r+1)) (eliminated: ${eliminated_this}) ──"
      else
        echo "  ── Round $((r+1)) (final) ──"
      fi
      # Print candidate votes sorted descending.
      echo "${vote_rounds_json}" | jq -r ".[${r}].candidate_votes | to_entries | sort_by(-.value)[] | \"    \(.key): \(.value) vote(s)\"" 2>/dev/null
    done
  fi
  echo ""

  # ── Session conclusions ────────────────────────────────────────────────
  echo "═══ Session Conclusions ═══"
  for ((i=1; i<=size; i++)); do
    local sid="s${i}"
    local slog="${rundir}/${sid}.log"
    local perspective_idx=$(( (i-1) % 4 ))
    local persp="${perspectives[${perspective_idx}]}"

    if [ ! -f "${slog}" ]; then
      echo "  ── ${sid} (${persp}) ──"
      echo "    (no log file)"
      echo ""
      continue
    fi

    echo "  ── ${sid} (${persp}) ──"

    # Strategy: find last meaningful paragraph (>80 chars, bounded by blank lines).
    # Use awk in paragraph mode (RS='') to collect blocks, pick last substantial one.
    local conclusion
    conclusion=$(awk -v RS='' '
      length($0) >= 80 { last = $0 }
      END {
        if (last != "") {
          gsub(/^[[:space:]]+|[[:space:]]+$/, "", last)
          print last
        }
      }
    ' "${slog}" 2>/dev/null)

    if [ -n "${conclusion}" ]; then
      # Truncate to ~12 lines (roughly 740 chars) for readability.
      local truncated
      truncated=$(echo "${conclusion}" | head -c 740)
      echo "${truncated}" | fold -s -w 74 | awk '{print "    " $0}'
      if [ "${#conclusion}" -gt 740 ]; then
        echo "    [...]"
      fi
    else
      # Fallback: last heading section or last non-empty lines.
      local last_section
      last_section=$(grep -n "^## " "${slog}" 2>/dev/null | tail -1 | cut -d: -f1)
      if [ -n "${last_section}" ]; then
        tail -n +"${last_section}" "${slog}" 2>/dev/null \
          | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' \
          | tail -n 12 \
          | awk '{print "    " $0}'
      else
        grep -v '^[[:space:]]*$' "${slog}" 2>/dev/null | tail -n 10 \
          | awk '{print "    " $0}'
      fi
    fi
    echo ""
  done

  # ── Session output summary ─────────────────────────────────────────────
  echo "═══ Session Outputs ═══"
  for ((i=1; i<=size; i++)); do
    local sdir="${rundir}/sessions/s${i}/output"
    if [ -d "${sdir}" ] && [ "$(ls -A "${sdir}" 2>/dev/null)" ]; then
      echo "  s${i} output:"
      ls -1 "${sdir}" 2>/dev/null | awk '{print "    " $0}'
    fi
  done
  echo ""

  # ── Test spec reference ───────────────────────────────────────────────────
  if [ -f "${rundir}/test_spec.md" ]; then
    echo "═══ Test Specification ═══"
    echo "  Test spec: ${rundir}/test_spec.md"
    echo "  (read-only during EXECUTE)"
    echo ""
  fi

  # ── Meta-synthesis (conditional on --summarize) ──────────────────────
  if [ "${SUMMARIZE}" = "1" ]; then
    meta_synthesize "${rundir}" "${size}" "${port}"
  fi

  echo "═══════════════════════════════════════════════════════════════════"
  echo "  See session logs for full details: ${rundir}/"
  echo ""
}

synthesize_results "${BUS_PORT}" "${RUN_DIR}" "${BUS_LOG}" "${SWARM_SIZE}" "${FAILURE_DETECTED}"

# ── Token budget final report ────────────────────────────────────────────
track_tokens "synthesis" "${SWARM_SIZE}" "50"

event "Swarm run ${RUN_ID} complete."

# ── Final checkpoint ──────────────────────────────────────────────────────
write_checkpoint "SYNTHESIS"

event "  htop log: ${RUN_DIR}/htop.log"
