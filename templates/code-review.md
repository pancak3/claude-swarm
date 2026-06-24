# Code Review Task Template

Review the code at the given path for correctness bugs,
security vulnerabilities, performance issues, and style problems.

## Recommended Swarm Configuration

- **Swarm size**: 3-5
- **Phases**: PROPOSE (30s), CRITIQUE (45s), REBUTTAL (30s), VOTE (20s), EXECUTE (60s)
- **Perspectives**: correctness, security, performance, simplicity

## Example Prompt

```
swarm-orchestrate.sh "Review src/auth/login.ts for security vulnerabilities
and logic errors" 4
```

## Phase Prompts

### PROPOSE
Review the target code for: 1) logic errors and edge cases,
2) security vulnerabilities (XSS, injection, auth bypass),
3) performance anti-patterns, 4) style/maintainability issues.
Provide specific line references.

### EXECUTE
Apply all confirmed fixes for bugs and security issues found
during the debate. Style-only changes are optional. Add
regression tests for each fix.

## Output Expectations

- List of bugs found with severity (CRITICAL/HIGH/MEDIUM/LOW)
- Security vulnerabilities with CWE references where applicable
- Suggested fixes with line numbers
- Cleaned up code or patch file
