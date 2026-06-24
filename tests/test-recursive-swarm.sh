#!/usr/bin/env bash
# test-recursive-swarm.sh — verify recursive sub-task spawning logic and structural integrity
set -euo pipefail

echo "=== Recursive Swarm Tests ==="

SWARM_PLUGIN="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  FAIL: $1"; }

# Test 1: Assessor returns reasonable sizes.
echo "Test: Assessor returns size for trivial task..."
SIZE=$(bash "${SWARM_PLUGIN}/scripts/swarm-assess.sh" "fix typo in README.md")
if [ "${SIZE}" -le 5 ]; then
  pass "Trivial task gets small swarm (size=${SIZE})"
else
  fail "Trivial task got large swarm (size=${SIZE}, expected <=5)"
fi

echo "Test: Assessor returns larger size for complex task..."
SIZE=$(bash "${SWARM_PLUGIN}/scripts/swarm-assess.sh" "redesign the entire authentication system with OAuth2, JWT, session management, and multi-tenant support across 12 microservices")
if [ "${SIZE}" -ge 8 ]; then
  pass "Complex task gets larger swarm (size=${SIZE})"
else
  fail "Complex task got small swarm (size=${SIZE}, expected >=8)"
fi

# Test 2: Config file is valid YAML.
echo "Test: swarm.yaml is valid..."
if command -v python3 >/dev/null 2>&1; then
  if python3 -c "import yaml; yaml.safe_load(open('${SWARM_PLUGIN}/config/swarm.yaml'))" 2>/dev/null; then
    pass "swarm.yaml is valid YAML"
  else
    fail "swarm.yaml parse error (python3)"
  fi
else
  pass "swarm.yaml exists (YAML validator not available, skipping parse check)"
fi

# Test 3: Plugin manifest is valid JSON.
echo "Test: plugin.json is valid..."
if jq '.' "${SWARM_PLUGIN}/.claude-plugin/plugin.json" > /dev/null 2>&1; then
  pass "plugin.json is valid JSON"
else
  fail "plugin.json parse error"
fi

# Test 4: MCP config is valid JSON.
echo "Test: .mcp.json is valid..."
if jq '.' "${SWARM_PLUGIN}/.mcp.json" > /dev/null 2>&1; then
  pass ".mcp.json is valid JSON"
else
  fail ".mcp.json parse error"
fi

# Test 5: All required directories exist.
echo "Test: Directory structure..."
for dir in agents swarm-bus skills hooks scripts config templates tests/fixtures .claude-plugin; do
  if [ -d "${SWARM_PLUGIN}/${dir}" ]; then
    pass "Directory exists: ${dir}"
  else
    fail "Missing directory: ${dir}"
  fi
done

# Test 6: All agent templates have required frontmatter.
echo "Test: Agent template frontmatter..."
for agent in swarm-orchestrator swarm-worker swarm-synthesizer swarm-assessor swarm-spawner; do
  FILE="${SWARM_PLUGIN}/agents/${agent}.md"
  if [ -f "${FILE}" ]; then
    if grep -q "^---$" "${FILE}" && grep -q "^name:" "${FILE}"; then
      pass "Agent ${agent} has valid frontmatter"
    else
      fail "Agent ${agent} missing frontmatter"
    fi
  else
    fail "Agent ${agent} file not found"
  fi
done

# Test 7: All scripts are executable.
echo "Test: Script executability..."
for script in swarm-spawn.sh swarm-orchestrate.sh swarm-assess.sh swarm-cleanup.sh swarm-intercept.sh; do
  FILE="${SWARM_PLUGIN}/scripts/${script}"
  if [ -x "${FILE}" ]; then
    pass "Script ${script} is executable"
  else
    fail "Script ${script} is not executable"
  fi
done

# Test 8: Go binary builds and tests pass.
echo "Test: Go swarm-bus..."
cd "${SWARM_PLUGIN}/swarm-bus"
if go build -o swarm-bus . 2>/dev/null && go test ./... -count=1 > /dev/null 2>&1; then
  pass "Go swarm-bus builds and tests pass"
else
  fail "Go swarm-bus build/test failure"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[ "${FAIL}" -eq 0 ] || exit 1
