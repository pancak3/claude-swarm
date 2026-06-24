# Research / Design Task Template

Explore an open-ended question, research topic, or architectural
design problem. The swarm debates approaches and produces a
structured analysis.

## Recommended Swarm Configuration

- **Swarm size**: 5-8
- **Phases**: PROPOSE (45s), CRITIQUE (60s), REBUTTAL (45s), VOTE (30s), SYNTHESIS (45s)
- **Fast path**: no (full parliamentary debate)
- **Perspectives**: correctness, simplicity, performance, security

## Example Prompt

```
swarm-orchestrate.sh "Design an architecture for a real-time
collaborative document editor" 6
```

## Phase Prompts

### PROPOSE
Present your design approach covering: 1) system architecture
and data flow, 2) key trade-offs and why you chose this path,
3) failure modes and mitigations, 4) evaluation criteria.

### CRITIQUE
Evaluate each proposal for: completeness, consistency with
stated goals, awareness of edge cases, feasibility of
implementation. Flag any fatal flaws.

### EXECUTE
Produce the final design document incorporating the best
elements from the debate. Include architecture diagram
(ASCII), API surface, data model, and deployment strategy.

## Output Expectations

- Architecture decision record (ADR)
- Trade-off analysis table
- Recommended approach with rationale
- Open questions for future investigation
