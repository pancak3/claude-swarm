#!/usr/bin/env bash
# swarm-cleanup.sh — kill orphaned swarm processes and clean state
# Usage: swarm-cleanup.sh [--force] [--global]
#   --force:   kill processes (otherwise just list them)
#   --global:  clean across all projects (default: current project only)
set -euo pipefail

FORCE=""
GLOBAL=false
for arg in "$@"; do
  case "$arg" in
    --force) FORCE="--force" ;;
    --global) GLOBAL=true ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"
PROJECT_DIR="${SWARM_PROJECT_DIR:-$(pwd)}"

# ── Find orphaned processes via PID files in .swarm-state directories ──────
find_swarm_pids() {
  local search_dir="$1"
  if [ ! -d "${search_dir}" ]; then return; fi
  for pidfile in $(find "${search_dir}" -maxdepth 3 -name 'bus.pid' 2>/dev/null); do
    local pid
    pid=$(cat "${pidfile}" 2>/dev/null)
    if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
      # Verify it's actually a swarm-bus process
      if ps -p "${pid}" -o comm= 2>/dev/null | grep -q "swarm-bus"; then
        local rundir
        rundir=$(dirname "${pidfile}")
        echo "${pid} (run: ${rundir})"
      fi
    fi
  done
}

echo "[cleanup] Scanning for orphaned swarm processes..."

SWARM_ENTRIES=""
if ${GLOBAL}; then
  # Scan all .swarm-state directories in common locations
  SWARM_ENTRIES=$(find_swarm_pids "${PLUGIN_ROOT}/.swarm-state" 2>/dev/null || true)
  SWARM_ENTRIES="${SWARM_ENTRIES}$(find_swarm_pids "${PROJECT_DIR}/.swarm-state" 2>/dev/null || true)"
  SWARM_ENTRIES="${SWARM_ENTRIES}$(find /home -maxdepth 5 -name '.swarm-state' -type d 2>/dev/null | head -20 | while read d; do find_swarm_pids "$d" 2>/dev/null; done || true)"
else
  SWARM_ENTRIES=$(find_swarm_pids "${PROJECT_DIR}/.swarm-state" 2>/dev/null || true)
fi

SWARM_ENTRIES=$(echo "${SWARM_ENTRIES}" | grep -v '^[[:space:]]*$' || true)

if [ -z "${SWARM_ENTRIES}" ]; then
  echo "[cleanup] No orphaned swarm processes found."
else
  COUNT=$(echo "${SWARM_ENTRIES}" | wc -l)
  echo "[cleanup] Found ${COUNT} orphaned swarm process(es):"
  echo "${SWARM_ENTRIES}"
  if [ "${FORCE}" = "--force" ]; then
    echo "${SWARM_ENTRIES}" | awk '{print $1}' | while read pid; do
      kill "${pid}" 2>/dev/null || true
      echo "[cleanup] Killed PID ${pid}"
    done
    sleep 1
    # Force kill any stragglers
    echo "${SWARM_ENTRIES}" | awk '{print $1}' | while read pid; do
      kill -9 "${pid}" 2>/dev/null || true
    done
    echo "[cleanup] All orphaned swarm processes terminated."
  else
    echo "[cleanup] Use --force to kill. Add --global to scan all projects."
  fi
fi

# ── Clean old state dirs (older than 7 days) ───────────────────────────────
clean_old_dirs() {
  local search_dir="$1"
  if [ ! -d "${search_dir}" ]; then return; fi
  find "${search_dir}" -maxdepth 1 -type d -name 'swarm-*' -mtime +7 2>/dev/null | while read d; do
    echo "[cleanup] Removing stale: $(basename "${d}")"
    rm -rf "${d}"
  done
}

echo "[cleanup] Cleaning stale state directories..."
clean_old_dirs "${PROJECT_DIR}/.swarm-state"
if ${GLOBAL}; then
  clean_old_dirs "${PLUGIN_ROOT}/.swarm-state"
fi

# P0.2: Scan for and quarantine stray backup files left by worker sessions.
echo "[cleanup] Scanning for stray backup files..."
STRAY_COUNT=0
ARTIFACT_DIR="${PROJECT_DIR}/.swarm-state/artifacts"
for pattern in '*.bak_s[0-9]*' '*_backup' '*_s[0-9]*_final' '*_s[0-9]*_backup'; do
  for f in ${pattern}; do
    if [ -f "${f}" ]; then
      mkdir -p "${ARTIFACT_DIR}"
      dest="${ARTIFACT_DIR}/$(basename "${f}")"
      mv "${f}" "${dest}" 2>/dev/null && STRAY_COUNT=$((STRAY_COUNT + 1))
      echo "[cleanup] Quarantined: ${f} → ${dest}"
    fi
  done
done
if [ "${STRAY_COUNT}" -gt 0 ]; then
  echo "[cleanup] Quarantined ${STRAY_COUNT} stray backup files → ${ARTIFACT_DIR}/"
else
  echo "[cleanup] No stray backup files found."
fi

echo "[cleanup] Done."
