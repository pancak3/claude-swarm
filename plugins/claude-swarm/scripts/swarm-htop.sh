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
  # Running directly, not sourced.

  # ── Auto-discover running swarm-bus instances ─────────────────────────────
  # Strategy: search .swarm-state/*/ in cwd + up to 3 parent dirs,
  # extract port from bus.log for each active PID, verify with curl.
  discover_buses() {
    local discovered="" search_dirs="" d

    # Build search path: cwd + up to 3 parent dirs.
    search_dirs="${PWD}"
    d="${PWD}"
    for __ in 1 2 3; do
      d="${d%/*}"
      [ -n "${d}" ] && search_dirs="${search_dirs}"$'\n'"${d}"
    done
    search_dirs=$(echo "${search_dirs}" | sort -u)

    while IFS= read -r sdir; do
      [ -z "${sdir}" ] && continue
      local ss="${sdir}/.swarm-state"
      [ ! -d "${ss}" ] && continue
      for rundir in "${ss}"/swarm-*; do
        [ ! -d "${rundir}" ] && continue
        local pid_file="${rundir}/bus.pid"
        local bus_log="${rundir}/bus.log"
        [ ! -f "${pid_file}" ] && continue
        [ ! -f "${bus_log}" ] && continue

        local pid
        pid=$(cat "${pid_file}" 2>/dev/null || echo "")
        [ -z "${pid}" ] && continue

        # Verify PID is alive and is a swarm-bus.
        if ! kill -0 "${pid}" 2>/dev/null; then continue; fi
        if ! ps -p "${pid}" -o comm= 2>/dev/null | grep -q "swarm-bus"; then continue; fi

        local port
        port=$(grep -oE "SWARM_BUS_PORT=[0-9]+" "${bus_log}" 2>/dev/null | head -1 | cut -d= -f2)
        [ -z "${port}" ] && continue

        # Quick liveness check.
        if ! curl -sf --max-time 1 "http://127.0.0.1:${port}/status" >/dev/null 2>&1; then continue; fi

        local task_info
        task_info=$(grep "task:" "${bus_log}" 2>/dev/null | head -1 | sed 's/.*task: //' | cut -c1-60)
        [ -z "${task_info}" ] && task_info="(unknown task)"

        local run_ts
        run_ts=$(basename "${rundir}" | sed 's/^swarm-//')

        discovered="${discovered}${port}	${run_ts}	${task_info}"$'\n'
      done
    done <<< "${search_dirs}"

    # Fallback: scan for swarm-bus PIDs not found via .swarm-state (e.g. custom ports).
    # Collect known PIDs from discovered entries.
    local known_pids=""
    while IFS= read -r line; do
      local dport; dport=$(echo "${line}" | cut -f1)
      [ -z "${dport}" ] && continue
      known_pids="${known_pids} ${dport}"
    done <<< "${discovered}"

    if command -v ss >/dev/null 2>&1; then
      local bus_pids; bus_pids=$(pgrep -f 'swarm-bus' 2>/dev/null || true)
      for pid in ${bus_pids}; do
        local ss_port; ss_port=$(ss -tlnp 2>/dev/null | grep "pid=${pid}" | grep -oE ':[0-9]+' | head -1 | tr -d ':')
        [ -z "${ss_port}" ] && continue
        # Skip if port already in discovered list.
        if echo " ${known_pids} " | grep -q " ${ss_port} "; then continue; fi
        if curl -sf --max-time 1 "http://127.0.0.1:${ss_port}/status" >/dev/null 2>&1; then
          discovered="${discovered}${ss_port}	?	(discovered via ss)"$'\n'
          known_pids="${known_pids} ${ss_port}"
        fi
      done
    fi

    # Remove empty lines and deduplicate by port.
    echo "${discovered}" | grep -v '^$' | sort -t$'\t' -k1 -u
  }

  # ── Interactive arrow-key selector (up/down + enter) ──────────────────────
  # Usage: interactive_select <entries_tsv>
  # Returns: index (1-based) of selected entry.
  interactive_select() {
    local entries="$1"
    local count
    count=$(echo "${entries}" | wc -l)
    local selected=1

    # Save terminal state, switch to raw mode.
    local saved_stty; saved_stty=$(stty -g 2>/dev/null || echo "")
    stty raw -echo min 0 time 0 2>/dev/null || true

    _cleanup_sel() {
      [ -n "${saved_stty}" ] && stty "${saved_stty}" 2>/dev/null || stty sane 2>/dev/null
      echo ""
    }
    trap _cleanup_sel EXIT

    while true; do
      # Move cursor up <count> lines, redraw all options.
      if [ "${selected}" -gt 1 ]; then
        printf '\033[%dA' "$((selected - 1))"
      fi

      local i=1
      while IFS=$'\t' read -r port ts task; do
        [ -z "${port}" ] && continue
        local marker=" "
        if [ "${i}" -eq "${selected}" ]; then marker=">"; else marker=" "; fi
        # Clear line, print option.
        printf '\033[2K'
        printf " %s [%s] %-8s  %s\n" "${marker}" "${ts}" ":${port}" "${task}"
        i=$((i + 1))
      done <<< "${entries}"

      # Read a single key.
      local key; key=$(dd bs=3 count=1 2>/dev/null || echo "")

      case "${key}" in
        $'\033[A') # Up arrow
          if [ "${selected}" -gt 1 ]; then
            # Move cursor up.
            printf '\033[%dA' "$((count - selected + 1))"
            selected=$((selected - 1))
          fi
          ;;
        $'\033[B') # Down arrow
          if [ "${selected}" -lt "${count}" ]; then
            selected=$((selected + 1))
          else
            # Already at bottom, reposition to redraw.
            printf '\033[%dA' "$count"
            selected=1
          fi
          ;;
        $'\r'|$'\n'|'') # Enter or empty (timeout)
          # Move cursor below options and exit loop.
          printf '\033[%dB' "$((count - selected))"
          break
          ;;
        *)
          # Ignore other keys.
          ;;
      esac
    done

    trap - EXIT
    _cleanup_sel
    echo "${selected}"
  }

  # ── Discover buses ────────────────────────────────────────────────────────
  BUS_LIST=$(discover_buses)
  BUS_COUNT=$(echo "${BUS_LIST}" | grep -c '^' 2>/dev/null || echo 0)

  if [ "${BUS_COUNT}" -eq 0 ]; then
    echo "[$(date +%H:%M:%S)] htop: no swarm-bus instances found."
    echo ""
    echo "  Searched .swarm-state/ in: ${PWD} and up to 3 parent dirs."
    echo "  Check that a swarm is running (swarm-orchestrate.sh)."
    echo ""
    echo "  You can still specify a port manually:"
    echo "    $(basename "$0") <port>"
    exit 1
  fi

  if [ "${BUS_COUNT}" -eq 1 ]; then
    BUS_PORT=$(echo "${BUS_LIST}" | cut -f1)
    BUS_TASK=$(echo "${BUS_LIST}" | cut -f3)
    echo "[$(date +%H:%M:%S)] htop: auto-selected bus :${BUS_PORT} (${BUS_TASK})"
  else
    echo "[$(date +%H:%M:%S)] htop: ${BUS_COUNT} swarm-bus instances found."
    echo "  Use ↑/↓ to select, Enter to confirm."
    echo ""
    SELECTED_IDX=$(interactive_select "${BUS_LIST}")
    BUS_PORT=$(echo "${BUS_LIST}" | sed -n "${SELECTED_IDX}p" | cut -f1)
    BUS_TASK=$(echo "${BUS_LIST}" | sed -n "${SELECTED_IDX}p" | cut -f3)
    echo "  Selected bus :${BUS_PORT} (${BUS_TASK})"
  fi

  echo ""
  echo "[$(date +%H:%M:%S)] htop: polling bus :${BUS_PORT}/status every 2s"
  while true; do
    compact_poll "${BUS_PORT}"
    sleep 2
  done
fi
