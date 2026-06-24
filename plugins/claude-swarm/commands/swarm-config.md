---
description: View or modify Claude Swarm configuration
argument-hint: "[get|set <key> <value>]"
allowed-tools: Bash, Read, Write
---

Manage the Claude Swarm configuration at `${CLAUDE_PLUGIN_ROOT}/config/swarm.yaml`.

## If no arguments: Show current config

Read and display the config file:
```bash
cat ${CLAUDE_PLUGIN_ROOT}/config/swarm.yaml
```

## If `set <key> <value>`: Update a config value

Read `${CLAUDE_PLUGIN_ROOT}/config/swarm.yaml`, modify the specified key, and write the updated file.

Supported keys and their defaults:

| Key | Default | Description |
|-----|---------|-------------|
| max_sessions | 128 | Maximum concurrent sessions |
| orchestrator_count | 3 | Triumvirate size |
| depth_limit | 5 | Maximum recursion depth |
| min_sessions | 3 | Minimum sessions before respawn |
| model | claude-sonnet-4-6 | Model for worker sessions |
| deadlock_max_retries | 3 | Maximum deadlock retries before escalation |

## If `reset`: Reset to defaults

Write the default configuration back to the file.
