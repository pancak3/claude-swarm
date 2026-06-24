---
description: Open a question for parliamentary debate across multiple Claude sessions
argument-hint: "<question> [--size N]"
allowed-tools: Bash
---

Run a parliamentary debate on the user's question using multiple independent Claude sessions communicating via the Swarm Bus MCP server.

## Steps

### 1. Parse arguments

Extract the question from the user's input. If `--size N` is specified, use N as the swarm size. Otherwise, default to 8.

### 2. Improve the question for parliamentary debate

The user's raw question may be vague, single-word, or missing context. **Before passing it to the swarm, rewrite it** into a well-formed debate question that:

- Expands the core intent into a clear sentence or short paragraph
- Adds 1-2 specific aspects for the perspectives to examine (correctness, simplicity, performance, security)
- Uses as many words as needed — sessions have large context windows
- Does NOT add implementation instructions, code, or bias toward a particular answer

Set the improved question as `QUESTION`. If the user included `--size N`, set `SIZE` to N; otherwise default to 8.

### 3. Build the Swarm Bus (if needed)

```bash
cd ${CLAUDE_PLUGIN_ROOT}/swarm-bus && go build -o swarm-bus . 2>&1
```

### 4. Run the debate (synchronously — output shown when complete)

```bash
bash ${CLAUDE_PLUGIN_ROOT}/scripts/swarm-orchestrate.sh "${QUESTION}" ${SIZE}
```

The script produces a live ANSI htop tree in captured output plus timestamped events on stderr.

### 4. Show results

After the swarm completes, the orchestration script prints a full FINAL SYNTHESIS block (winning proposal, IRV vote tally, session conclusions) to stdout. **Show this output directly to the user — do not summarize it.**

Session logs are at `.swarm-state/swarm-<timestamp>/` in the project directory. The htop ANSI tree is visible in the captured output. The `htop.log` file has plain-text progress snapshots.
