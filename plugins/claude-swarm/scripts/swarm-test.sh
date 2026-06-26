#!/usr/bin/env bash
# swarm-test.sh — run a swarm debate and validate critical checkpoints.
# Usage: swarm-test.sh <task> [size]
# Exits 0 if all checkpoints pass, 1 if any fail.
set -euo pipefail

TASK="${1:-simple test}"
SIZE="${2:-3}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORCH="${SCRIPT_DIR}/swarm-orchestrate.sh"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "SWARM TEST: ${TASK} (size=${SIZE})"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

START_TS=$(date +%s)
PASS=0
FAIL=0
CHECKS=()

check() {
  local label="$1" condition="$2"
  printf "  [....] %s ... " "${label}"
  if eval "${condition}" 2>/dev/null; then
    echo "PASS"
    PASS=$((PASS + 1))
    CHECKS+=("PASS: ${label}")
  else
    echo "FAIL"
    FAIL=$((FAIL + 1))
    CHECKS+=("FAIL: ${label}")
  fi
}

# ── Run the debate ──────────────────────────────────────────────────────────
echo ""
echo "--- Running debate ---"
OUTPUT=$(bash "${ORCH}" "${TASK}" "${SIZE}" 2>&1) || true
END_TS=$(date +%s)
ELAPSED=$((END_TS - START_TS))

# ── Extract key metrics ────────────────────────────────────────────────────
LOG_DIR=$(echo "${OUTPUT}" | grep "Run dir:" | head -1 | sed 's/.*Run dir: //')
SPAWNED=$(echo "${OUTPUT}" | grep -c "Spawning session" || echo 0)
REGISTERED=$(echo "${OUTPUT}" | grep "session(s) registered" | tail -1 | sed 's/.* \([0-9]*\) session.*/\1/')
PROPOSALS=$(echo "${OUTPUT}" | grep "Proposals submitted:" | tail -1 | sed 's/.*Proposals submitted: *\([0-9]*\).*/\1/')
VOTES=$(echo "${OUTPUT}" | grep "Votes cast:" | tail -1 | sed 's/.*Votes cast: *\([0-9]*\).*/\1/')
COST=$(echo "${OUTPUT}" | grep "Est. cost" | tail -1 | sed 's/.*cost.*: //')
ERR_COUNT=$(echo "${OUTPUT}" | grep -c "ERROR" || echo 0)

echo ""
echo "--- Results (${ELAPSED}s elapsed) ---"

# ── Checkpoints ─────────────────────────────────────────────────────────────
check "Bus started"           'echo "${OUTPUT}" | grep -q "Swarm Bus started"'
check "Sessions spawned"      '[ "${SPAWNED:-0}" -ge "${SIZE}" ]'
check "Sessions registered"   '[ "${REGISTERED:-0}" -ge 1 ]'
check "Registration quorum"   'echo "${OUTPUT}" | grep -q "quorum met\|session(s) registered\."'
check "Proposals submitted"   '[ "${PROPOSALS:-0}" -ge 1 ]'
check "Votes cast"            '[ "${VOTES:-0}" -ge 1 ]'
check "Winner determined"     'echo "${OUTPUT}" | grep -q "Winning Proposal"'
check "Clean exit"            'echo "${OUTPUT}" | grep -q "Swarm Bus stopped"'
check "No ERROR lines"        '[ "${ERR_COUNT:-0}" -eq 0 ]'
check "No hang (completed)"   'echo "${OUTPUT}" | grep -q "Swarm run.*complete"'

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "RESULTS: ${PASS} passed, ${FAIL} failed in ${ELAPSED}s"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
for c in "${CHECKS[@]}"; do echo "  $c"; done
echo ""
echo "Log: ${LOG_DIR:-unknown}"
echo "Cost: ${COST:-N/A}"
echo "Registered: ${REGISTERED:-0}  Proposals: ${PROPOSALS:-0}  Votes: ${VOTES:-0}"

[ ${FAIL} -eq 0 ] && exit 0 || exit 1
