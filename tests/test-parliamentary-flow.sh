#!/usr/bin/env bash
# test-parliamentary-flow.sh — end-to-end test of the parliamentary protocol
set -euo pipefail

echo "=== Parliamentary Flow E2E Tests ==="

SWARM_BUS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../swarm-bus" && pwd)"
cd "${SWARM_BUS_DIR}"
go build -o swarm-bus .

PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  FAIL: $1 — $2"; }

echo "Test: Go unit tests cover full parliamentary flow..."
if go test ./... -run "TestProposeThenCritiqueFlow|TestFatalFlawElimination|TestTallySimpleMajority|TestTallyInstantRunoff|TestValidateProposal" -v -count=1 > /tmp/swarm-parliamentary-test.txt 2>&1; then
  pass "Parliamentary flow unit tests pass"
else
  fail "Parliamentary flow unit tests" "$(tail -5 /tmp/swarm-parliamentary-test.txt)"
fi

echo "Test: Verify state machine starts in REGISTERING..."
if go test ./... -run "TestMachineStartsInRegistering" -v -count=1 > /tmp/swarm-parliamentary-test.txt 2>&1; then
  pass "State machine initializes correctly"
else
  fail "State machine initialization" "$(tail -3 /tmp/swarm-parliamentary-test.txt)"
fi

echo "Test: Session registration and duplicate prevention..."
if go test ./... -run "TestSessionRegistration" -v -count=1 > /tmp/swarm-parliamentary-test.txt 2>&1; then
  pass "Session registration works"
else
  fail "Session registration" "$(tail -3 /tmp/swarm-parliamentary-test.txt)"
fi

echo "Test: Wrong-round submission rejection..."
if go test ./... -run "TestSubmitProposalInWrongRound" -v -count=1 > /tmp/swarm-parliamentary-test.txt 2>&1; then
  pass "Wrong-round submissions rejected"
else
  fail "Wrong-round rejection" "$(tail -3 /tmp/swarm-parliamentary-test.txt)"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[ "${FAIL}" -eq 0 ] || exit 1
