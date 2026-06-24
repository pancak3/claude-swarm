---
description: Activate swarm mode — all tasks execute via fractal multi-session parliamentary deliberation
argument-hint: "[task-description]"
allowed-tools: Bash
---

Activate Claude Swarm mode. When active, every task is debated by multiple independent Claude sessions before execution.

## Steps

### 1. Build the Swarm Bus

```bash
cd ${CLAUDE_PLUGIN_ROOT}/swarm-bus && go build -o swarm-bus .
```

### 2. Export SWARM_MODE

Run:
```bash
export SWARM_MODE=1
```

Then confirm: "Swarm mode activated. All tasks will now route through parliamentary deliberation."

### 3. If a task was provided, run it now

If the user provided a task description as an argument, assess the optimal swarm size and orchestrate:

```bash
TASK="<user's task description>"
SIZE=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/swarm-assess.sh "${TASK}")
echo "Assessed swarm size: ${SIZE}"
bash ${CLAUDE_PLUGIN_ROOT}/scripts/swarm-orchestrate.sh "${TASK}" ${SIZE}
```

### 4. For subsequent tasks in this session

The hook in hooks/hooks.json intercepts tool calls when SWARM_MODE=1 and routes them through the swarm.

### 5. To deactivate

```bash
export SWARM_MODE=0
```
