#!/usr/bin/env bash
# swarm-assess.sh — determine optimal swarm size for a task
# Usage: swarm-assess.sh <task-description>

TASK="${1:?task-description required}"

# Count words in task.
WORD_COUNT=$(echo "${TASK}" | wc -w)

# P2.1: Triviality gate — skip the swarm for mechanical tasks.
if [ "${WORD_COUNT}" -le 30 ]; then
  HAS_DECISION=$(echo "${TASK}" | grep -iEo 'choose|decide|design|which|compare|evaluate|should|better|trade-off|implement|create|build|fix|review|audit|analyze|optimize|improve' 2>/dev/null | wc -l; true)
  HAS_DECISION=${HAS_DECISION:-0}
  if [ "${HAS_DECISION}" -eq 0 ]; then
    echo "0"  # skip swarm — single worker can handle
    exit 0
  fi
fi

# Count occurrences of complexity-indicating keywords (grep -o outputs each match on its own line).
COMPLEXITY_INDICATORS=$(echo "${TASK}" | grep -iEo 'architecture|refactor|migrat|system|design|complex|multiple|cross-cutting|redesign|rewrite|overhaul|distributed|scalable|microservice' 2>/dev/null | wc -l; true)

# Count occurrences of triviality-indicating keywords.
IS_TRIVIAL=$(echo "${TASK}" | grep -iEo 'typo|fix a bug|update.*version|bump|changelog|readme|comment' 2>/dev/null | wc -l; true)

# Ensure integer values.
COMPLEXITY_INDICATORS=${COMPLEXITY_INDICATORS:-0}
IS_TRIVIAL=${IS_TRIVIAL:-0}

if [ "${IS_TRIVIAL}" -gt 0 ] 2>/dev/null; then
  echo "3"
elif [ "${COMPLEXITY_INDICATORS}" -ge 3 ] 2>/dev/null || [ "${WORD_COUNT}" -gt 100 ]; then
  SIZE=$(( COMPLEXITY_INDICATORS * 4 + 4 ))
  [ "${SIZE}" -gt 32 ] && SIZE=32
  echo "${SIZE}"
elif [ "${WORD_COUNT}" -gt 30 ]; then
  echo "8"
else
  echo "5"
fi
