#!/usr/bin/env bash
# swarm-intercept.sh — intercept a task and route through swarm
set -euo pipefail

# Defense-in-depth: also check SWARM_MODE in case hook condition is unsupported.
if [ "${SWARM_MODE:-0}" != "1" ]; then
  echo "[swarm-intercept] SWARM_MODE not active, passing through."
  exit 0
fi

TASK="${1:-}"
if [ -z "${TASK}" ]; then
  echo "[swarm-intercept] No task provided, passing through."
  exit 0
fi

echo "[swarm-intercept] SWARM_MODE active. Routing task through fractal swarm..."
echo "[swarm-intercept] Task: ${TASK}"

SWARM_PLUGIN_ROOT="${SWARM_PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT}}"

# P2.1: Check for --force-swarm override.
FORCE_SWARM=0
if echo "${TASK}" | grep -q '\--force-swarm'; then
  FORCE_SWARM=1
  TASK=$(echo "${TASK}" | sed 's/--force-swarm//g' | sed 's/  */ /g' | sed 's/^ *//;s/ *$//')
fi

# Assess swarm size.
SWARM_SIZE=$(bash "${SWARM_PLUGIN_ROOT}/scripts/swarm-assess.sh" "${TASK}")
echo "[swarm-intercept] Assessed swarm size: ${SWARM_SIZE}"

# P2.1: Triviality gate — skip the swarm for mechanical tasks.
if [ "${SWARM_SIZE}" = "0" ] && [ "${FORCE_SWARM}" = "0" ]; then
  echo "[swarm-intercept] Task assessed as trivial — skipping swarm."
  echo "[swarm-intercept] Use --force-swarm to override, or /swarm-debate for explicit debate."
  exit 0
fi

# If forced, use a minimum size of 3.
if [ "${SWARM_SIZE}" = "0" ] && [ "${FORCE_SWARM}" = "1" ]; then
  SWARM_SIZE=3
  echo "[swarm-intercept] Triviality gate overridden (--force-swarm). Using size=${SWARM_SIZE}."
fi

# Spawn triumvirate orchestrators.
bash "${SWARM_PLUGIN_ROOT}/scripts/swarm-orchestrate.sh" "${TASK}" "${SWARM_SIZE}" "${SWARM_PLUGIN_ROOT}/.mcp.json"
