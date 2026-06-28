#!/usr/bin/env bash
# swarm-htop.sh — compact inline progress for swarm orchestration.
# When sourced: exports compact_poll() and fmt_cost() for the orchestrator.
# When run directly: polls bus and prints compact status lines to stdout.
set -euo pipefail

# Cost per million tokens (default: DeepSeek-V4-Pro pricing)
COST_IN_PER_M="${SWARM_COST_INPUT_PER_M:-0.435}"
COST_OUT_PER_M="${SWARM_COST_OUTPUT_PER_M:-0.87}"

# ── Shared utilities ────────────────────────────────────────────────────────

# Format token count as dollars/cents.
fmt_cost() {
  local tokens_in=${1:-0} tokens_out=${2:-0}
  python3 -c "
tin = ${tokens_in} / 1_000_000.0 * ${COST_IN_PER_M}
tout = ${tokens_out} / 1_000_000.0 * ${COST_OUT_PER_M}
total = tin + tout
if total >= 1.0:
    print(f'\${total:.2f}')
elif total >= 0.01:
    print(f'{total*100:.1f} cents')
else:
    print('<0.1c')
" 2>/dev/null || echo '-'
}

# Poll the bus and print a compact tree status block.
# Usage: compact_poll <bus-port> [expected-session-count]
compact_poll() {
  local port="${1:?bus port required}"
  local expected="${2:-0}"
  local data
  data=$(curl -sf --max-time 2 "http://127.0.0.1:${port}/status" 2>/dev/null || echo "")
  if [ -z "${data}" ]; then
    return
  fi
  local round active total props votes timeleft
  round=$(    echo "${data}" | jq -r '.round // "?"')
  active=$(   echo "${data}" | jq -r '.active_sessions // 0')
  total=$(    echo "${data}" | jq -r '.total_sessions // 0')
  props=$(    echo "${data}" | jq -r '.proposals_submitted // 0')
  votes=$(    echo "${data}" | jq -r '.votes_cast // 0')
  timeleft=$( echo "${data}" | jq -r '.time_remaining // "?"')

  local tin tout
  tin=$(  echo "${data}" | jq -r '[.sessions[]?.tokens_in // 0] | add // 0' 2>/dev/null || echo 0)
  tout=$( echo "${data}" | jq -r '[.sessions[]?.tokens_out // 0] | add // 0' 2>/dev/null || echo 0)
  local cost
  cost=$(fmt_cost "${tin}" "${tout}")

  # ── Header ──
  echo "── $(date +%H:%M:%S) ${round} | sessions=${active}/${total} | proposals=${props} | votes=${votes} | time=${timeleft} | cost=${cost} ──"

  # ── Session tree ──
  local sess_count
  sess_count=$(echo "${data}" | jq -r '.sessions | length // 0' 2>/dev/null || echo 0)
  if [ "${sess_count}" -gt 0 ]; then
    local idx=0
    while IFS= read -r line; do
      idx=$((idx + 1))
      local sid spersp sactive tok_in tok_out
      sid=$(    echo "${line}" | jq -r '.id // "?"')
      spersp=$( echo "${line}" | jq -r '.perspective // "?"')
      sactive=$(echo "${line}" | jq -r '.active // false')
      tok_in=$( echo "${line}" | jq -r '.tokens_in // 0')
      tok_out=$(echo "${line}" | jq -r '.tokens_out // 0')
      tok_in=${tok_in:-0}; tok_out=${tok_out:-0}

      local conn icon sstatus
      if [ "${idx}" -lt "${sess_count}" ]; then conn="├──"; else conn="└──"; fi
      if [ "${sactive}" = "true" ]; then icon="●"; sstatus="${round}"; else icon="○"; sstatus="inactive"; fi
      local scost; scost=$(fmt_cost "${tok_in}" "${tok_out}")

      printf "  %s %s %-5s %-12s in: %6s  out: %6s  cost: %s\n" \
        "${conn}" "${icon}" "${sid}" "${spersp}" \
        "$(printf "%'d" "${tok_in}" 2>/dev/null || echo "${tok_in}")" \
        "$(printf "%'d" "${tok_out}" 2>/dev/null || echo "${tok_out}")" \
        "${scost}"
    done < <(echo "${data}" | jq -c '.sessions | sort_by(.id)[]' 2>/dev/null)
  fi

  # Show expected-but-unregistered sessions.
  if [ "${expected}" -gt 0 ] 2>/dev/null; then
    local registered_ids; registered_ids=$(echo "${data}" | jq -r '[.sessions[].id] | join(" ")' 2>/dev/null || echo "")
    local has_unreg=false
    for ((i=1; i<=expected; i++)); do
      local sid="s${i}"
      if ! echo "${registered_ids}" | grep -qw "${sid}"; then
        if ! ${has_unreg}; then has_unreg=true; fi
        echo "  ○ ○ ${sid}  (awaiting registration)"
      fi
    done
  fi

  # ── Proposals (compact) ──
  local prop_count
  prop_count=$(echo "${data}" | jq -r '.proposals | length // 0' 2>/dev/null || echo 0)
  if [ "${prop_count}" -gt 0 ]; then
    local pidx=0
    while IFS= read -r pline; do
      pidx=$((pidx + 1))
      local pid pconf pelim
      pid=$(   echo "${pline}" | jq -r '.id // "?"')
      pconf=$(  echo "${pline}" | jq -r '.confidence // 0')
      pelim=$(  echo "${pline}" | jq -r '.eliminated // false')
      local pconn ptag
      if [ "${pidx}" -lt "${prop_count}" ]; then pconn="├──"; else pconn="└──"; fi
      if [ "${pelim}" = "true" ]; then ptag="✗"; else ptag="✓"; fi
	      local elim_flag=""; if [ "${pelim}" = "true" ]; then elim_flag=" (eliminated)"; fi
      echo "  ${pconn} ${ptag} ${pid}  conf: ${pconf}%${elim_flag}"
    done < <(echo "${data}" | jq -c '.proposals[]' 2>/dev/null)
  fi

  echo ""
}

# ── Direct execution mode (legacy, used by some workflows) ──────────────────
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  # Running directly, not sourced. Poll continuously.
  BUS_PORT="${1:?bus port required}"
  echo "[$(date +%H:%M:%S)] htop: polling bus :${BUS_PORT}/status every 2s (direct mode)"
  while true; do
    compact_poll "${BUS_PORT}"
    sleep 2
  done
fi
