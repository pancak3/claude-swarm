# Write Code Task Template

Implement a new feature or function following the specification.

## Recommended Swarm Configuration

- **Swarm size**: 3-8
- **Phases**: PROPOSE (30s), VOTE (20s), EXECUTE (120s), SYNTHESIS (30s)
- **Fast path**: yes (skip CRITIQUE/REBUTTAL)
- **Perspectives**: correctness, simplicity, performance, security

## Example Prompt

```
swarm-orchestrate.sh "Implement a rate-limited HTTP client
with exponential backoff in Python" 4
```

## Phase Prompts

### PROPOSE
Describe your implementation approach: 1) architecture and
module structure, 2) error handling strategy, 3) test plan,
4) API surface design. Include estimated sub-tasks.

### EXECUTE
Implement the agreed-upon design. Write clean, well-typed code
with tests. Follow the project's existing conventions.

## Output Expectations

- Working implementation
- Unit tests with >80% coverage
- Type hints and docstrings
- Error handling for edge cases
