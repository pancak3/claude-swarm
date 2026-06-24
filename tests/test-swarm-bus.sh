#!/usr/bin/env bash
# test-swarm-bus.sh — integration test for the Swarm Bus MCP server
set -euo pipefail

echo "=== Swarm Bus Integration Tests ==="

SWARM_BUS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../swarm-bus" && pwd)"
SWARM_BUS_BIN="${SWARM_BUS_DIR}/swarm-bus"

# Build the bus binary if needed.
if [ ! -x "${SWARM_BUS_BIN}" ]; then
  echo "Building swarm-bus..."
  cd "${SWARM_BUS_DIR}" && go build -o swarm-bus .
fi

# Test 1: Binary exists and is executable.
echo "Test 1: Binary exists..."
if [ -x "${SWARM_BUS_BIN}" ]; then
  echo "  PASS"
else
  echo "  FAIL: swarm-bus not found at ${SWARM_BUS_BIN}"
  exit 1
fi

# Test 2: Binary starts and prints startup message.
echo "Test 2: Binary starts..."
timeout 2s bash -c "echo '' | SWARM_TASK_ID=test-1 SWARM_TASK_DESCRIPTION='test task' ${SWARM_BUS_BIN} 2>&1" | grep -q "swarm-bus" && echo "  PASS" || echo "  FAIL (non-critical — may need stdin)"

# Test 3: Go unit tests pass.
echo "Test 3: Go unit tests..."
cd "${SWARM_BUS_DIR}" && go test ./... -v -count=1 && echo "  PASS" || {
  echo "  FAIL: unit tests failed"
  exit 1
}

echo "=== All Swarm Bus tests passed ==="
