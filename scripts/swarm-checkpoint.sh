#!/usr/bin/env bash
# swarm-checkpoint.sh — save/restore swarm state for checkpoint/resume
# Usage:
#   swarm-checkpoint.sh save <rundir> <phase> <port>    — save state after phase transition
#   swarm-checkpoint.sh load <rundir>                    — load checkpoint, output last completed phase

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"

CMD="${1:?command required: save or load}"

case "${CMD}" in
  save)
    RUNDIR="${2:?rundir required}"
    PHASE="${3:?phase required}"
    PORT="${4:-}"

    CHECKPOINT_FILE="${RUNDIR}/checkpoint.json"
    STATE_DIR="${RUNDIR}/checkpoints"
    mkdir -p "${STATE_DIR}"

    # Fetch current bus state
    BUS_JSON="{}"
    if [ -n "${PORT}" ]; then
      BUS_JSON=$(curl -sf --max-time 3 "http://127.0.0.1:${PORT}/status" 2>/dev/null || echo '{"error":"bus unreachable"}')
    fi

    # Build checkpoint manifest
    cat > "${CHECKPOINT_FILE}" << EOF
{
  "run_id": "$(basename "${RUNDIR}")",
  "checkpoint_time": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "last_completed_phase": "${PHASE}",
  "phases": {
    "REGISTER": {"completed": $( [ "${PHASE}" = "REGISTER" ] || [ "${PHASE}" = "PROPOSE" ] || [ "${PHASE}" = "VOTE" ] || [ "${PHASE}" = "EXECUTE" ] || [ "${PHASE}" = "SYNTHESIS" ] || [ "${PHASE}" = "CLOSED" ] && echo "true" || echo "false")},
    "PROPOSE":  {"completed": $( [ "${PHASE}" = "PROPOSE" ] || [ "${PHASE}" = "VOTE" ] || [ "${PHASE}" = "EXECUTE" ] || [ "${PHASE}" = "SYNTHESIS" ] || [ "${PHASE}" = "CLOSED" ] && echo "true" || echo "false")},
    "VOTE":     {"completed": $( [ "${PHASE}" = "VOTE" ] || [ "${PHASE}" = "EXECUTE" ] || [ "${PHASE}" = "SYNTHESIS" ] || [ "${PHASE}" = "CLOSED" ] && echo "true" || echo "false")},
    "EXECUTE":  {"completed": $( [ "${PHASE}" = "EXECUTE" ] || [ "${PHASE}" = "SYNTHESIS" ] || [ "${PHASE}" = "CLOSED" ] && echo "true" || echo "false")},
    "SYNTHESIS":{"completed": $( [ "${PHASE}" = "SYNTHESIS" ] || [ "${PHASE}" = "CLOSED" ] && echo "true" || echo "false")}
  },
  "bus_state": ${BUS_JSON},
  "phase_artifacts": {
    "proposals": $( [ -f "${RUNDIR}/proposals.json" ] && cat "${RUNDIR}/proposals.json" 2>/dev/null || echo "[]"),
    "votes": $( [ -f "${RUNDIR}/votes.json" ] && cat "${RUNDIR}/votes.json" 2>/dev/null || echo "[]")
  }
}
EOF

    # Also write phase-specific checkpoint file
    cp "${CHECKPOINT_FILE}" "${STATE_DIR}/${PHASE}.json"
    echo "Checkpoint saved for phase ${PHASE}: ${CHECKPOINT_FILE}"
    ;;

  load)
    RUNDIR="${2:?rundir required}"
    CHECKPOINT_FILE="${RUNDIR}/checkpoint.json"

    if [ ! -f "${CHECKPOINT_FILE}" ]; then
      echo "NO_CHECKPOINT"
      exit 0
    fi

    # Output the last completed phase so the orchestrator can skip completed phases
    LAST_PHASE=$(python3 -c "
import json
with open('${CHECKPOINT_FILE}') as f:
    data = json.load(f)
print(data.get('last_completed_phase', ''))
" 2>/dev/null || echo "")

    if [ -n "${LAST_PHASE}" ]; then
      echo "${LAST_PHASE}"
    else
      echo "NO_CHECKPOINT"
    fi
    ;;

  *)
    echo "Usage: $0 {save|load} <rundir> [phase] [port]"
    exit 1
    ;;
esac
