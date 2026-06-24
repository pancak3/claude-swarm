#!/usr/bin/env bash
# swarm-dashboard.sh — live status dashboard for swarm orchestration
# Polls the swarm-bus GET /status endpoint every 2 seconds and displays
# current round, session count, proposals, votes, and per-session registration.
# Usage: swarm-dashboard.sh <bus-port> <run-dir>
# Exits when round is CLOSED or on SIGTERM.
set -euo pipefail

BUS_PORT="${1:?bus port required}"
RUN_DIR="${2:?run dir required}"
LOG="${RUN_DIR}/dashboard.log"
STATUS_JSON="${RUN_DIR}/dashboard.status"

log()     { echo "[$(date '+%H:%M:%S')] $*" >> "${LOG}"; }
event()   { echo "[$(date '+%H:%M:%S')] dashboard: $*" >&2; }

# Helper: compute integer percentage, avoiding division by zero.
pct() {
  local n=$1 total=$2
  if [ "${total}" -le 0 ]; then echo "0"; else echo "$(( n * 100 / total ))"; fi
}

trap 'event "terminated"; exit 0' TERM INT

event "started, polling :${BUS_PORT}/status every 2s"
log "=== dashboard started ==="

prev_round=""
prev_proposals=-1
prev_votes=-1
prev_active=-1
prev_sessions=""

while true; do
  DATA=$(curl -sf --max-time 2 "http://127.0.0.1:${BUS_PORT}/status" 2>/dev/null || echo "")

  if [ -z "${DATA}" ]; then
    sleep 2
    continue
  fi

  # Persist raw JSON for inspection by the orchestration script or user
  echo "${DATA}" > "${STATUS_JSON}"

  ROUND=$(     echo "${DATA}" | jq -r '.round // "?"')
  ACTIVE=$(    echo "${DATA}" | jq -r '.active_sessions // 0')
  TOTAL=$(     echo "${DATA}" | jq -r '.total_sessions // 0')
  PROPS=$(     echo "${DATA}" | jq -r '.proposals_submitted // 0')
  CRITS=$(     echo "${DATA}" | jq -r '.critiques_submitted // 0')
  REBS=$(      echo "${DATA}" | jq -r '.rebuttals_submitted // 0')
  VOTES=$(     echo "${DATA}" | jq -r '.votes_cast // 0')
  TIMELEFT=$(  echo "${DATA}" | jq -r '.time_remaining // "?"')

  # Exit when swarm is complete
  if [ "${ROUND}" = "CLOSED" ]; then
    event "swarm complete (CLOSED)"
    log "=== swarm complete ==="
    exit 0
  fi

  # --- State-change events to stderr ---
  if [ "${ROUND}" != "${prev_round}" ]; then
    if [ -n "${prev_round}" ]; then
      event "round -> ${ROUND} (${TIMELEFT})"
    else
      event "round = ${ROUND} (${TIMELEFT})"
    fi
    log "--- round: ${ROUND} ---"
  fi

  if [ "${ACTIVE}" != "${prev_active}" ] && [ "${prev_active}" -ne -1 ]; then
    delta=$((ACTIVE - prev_active))
    if [ "${delta}" -gt 0 ]; then
      event "${delta} session(s) registered (${ACTIVE}/${TOTAL})"
    fi
  fi

  if [ "${PROPS}" != "${prev_proposals}" ] && [ "${prev_proposals}" -ne -1 ]; then
    delta=$((PROPS - prev_proposals))
    event "${delta} new proposal(s) (${PROPS}/${TOTAL} = $(pct "${PROPS}" "${TOTAL}")%)"
  fi

  if [ "${VOTES}" != "${prev_votes}" ] && [ "${prev_votes}" -ne -1 ]; then
    delta=$((VOTES - prev_votes))
    event "${delta} new vote(s) (${VOTES}/${ACTIVE} = $(pct "${VOTES}" "${ACTIVE}")%)"
  fi

  # --- Track active session IDs ---
  SESSION_ACTIVE_IDS=$(echo "${DATA}" | jq -r '.sessions[] | select(.active==true) | .id' 2>/dev/null | sort | tr '\n' ' ' | sed 's/ $//')
  if [ "${SESSION_ACTIVE_IDS}" != "${prev_sessions}" ]; then
    event "registered: ${SESSION_ACTIVE_IDS:-none yet}"
    prev_sessions="${SESSION_ACTIVE_IDS}"
  fi

  # --- Write formatted status display to log file ---
  {
    echo "┌─────────────────────────────────────────────────────────────────┐"
    echo "│                      SWARM LIVE DASHBOARD                       │"
    echo "├─────────────────────────────────────────────────────────────────┤"
    printf "│  Round:       %-51s│\n" "${ROUND}  (${TIMELEFT})"
    printf "│  Sessions:    %-51s│\n" "${ACTIVE} active / ${TOTAL} total"
    printf "│  Proposals:   %-51s│\n" "${PROPS}/${TOTAL} ($(pct "${PROPS}" "${TOTAL}")%)"
    printf "│  Critiques:   %-51s│\n" "${CRITS}/${TOTAL} ($(pct "${CRITS}" "${TOTAL}")%)"
    printf "│  Rebuttals:   %-51s│\n" "${REBS}/${TOTAL} ($(pct "${REBS}" "${TOTAL}")%)"
    printf "│  Votes:       %-51s│\n" "${VOTES}/${ACTIVE} ($(pct "${VOTES}" "${ACTIVE}")%)"
    echo "├─────────────────────────────────────────────────────────────────┤"
    echo "│  Session Registration Status                                    │"
    echo "├─────────────────────────────────────────────────────────────────┤"

    # Show registered sessions
    REG_COUNT=$(echo "${DATA}" | jq -r '.sessions | length // 0')
    if [ "${REG_COUNT}" -gt 0 ]; then
      while IFS= read -r line; do
        SID=$(echo "${line}" | jq -r '.id // ""')
        SPERSP=$(echo "${line}" | jq -r '.perspective // "?"')
        SACTIVE=$(echo "${line}" | jq -r '.active // false')
        if [ "${SACTIVE}" = "true" ]; then
          printf "│  v %-8s (%-12s) registered                          │\n" "${SID}" "${SPERSP}"
        else
          printf "│  x %-8s (%-12s) inactive                             │\n" "${SID}" "${SPERSP}"
        fi
      done < <(echo "${DATA}" | jq -c '.sessions[]' 2>/dev/null)
    fi

    # Compute and display expected-but-unregistered sessions
    REGISTERED_IDS=$(echo "${DATA}" | jq -r '.sessions[].id // empty' 2>/dev/null | sort)
    for ((i=1; i<=TOTAL; i++)); do
      sid="s${i}"
      if ! echo "${REGISTERED_IDS}" | grep -qxF "${sid}"; then
        printf "│  o s%-8s (--) awaiting registration                    │\n" "${i}"
      fi
    done

    if [ "${REG_COUNT}" -le 0 ]; then
      echo "│  (no sessions registered yet)                                   │"
    fi

    echo "└─────────────────────────────────────────────────────────────────┘"
  } >> "${LOG}"

  prev_round="${ROUND}"
  prev_proposals="${PROPS}"
  prev_votes="${VOTES}"
  prev_active="${ACTIVE}"

  sleep 2
done
