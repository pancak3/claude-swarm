# Swarm Parameter Role Pool

Each role is a (temperature, top_p) pair with a defined cognitive function.
Every debate **MUST** include Oracle (min) and Chaos (max). Remaining roles are selected from the pool based on the query type.

## Rule: 3 sessions per role

For each role selected, we spawn **3 sessions** with the same (temp, top_p) — one session for each of these perspectives:
- correctness, simplicity, performance

(This gives 3 independent "takes" on the same parameter regime for consensus.)

Security perspective is handled by Oracle and Chaos — they serve as boundaries.

**Session count formula**: total = (2 mandatory + N selected) × 3

---

## Mandatory Anchors (always included)

| # | Role | temp | top_p | Sessions | Cognitive Function |
|---|------|------|-------|----------|-------------------|
| A1 | **Oracle** | 0.00 | 0.00 | 3 | **Absolute determinism**. Always picks the single most probable token. No randomness at all. Serves as the "ground truth" baseline — if every debate included Oracle, you'd know what pure determinism produces. Side effect: extremely repetitive, may loop. |
| A2 | **Chaos** | 2.00 | 1.00 | 3 | **Maximum entropy**. Every token in the vocabulary is equally likely. Serves as the "random monkey" baseline — if Chaos produces something useful, the problem space is trivially easy. Side effect: often incoherent, sometimes brilliant by accident. |

These two bracket the entire useful range. Everything else is between them.

---

## Core Pool (select per query type)

### Precision Band (0.01–0.15 temp)

| # | Role | temp | top_p | Best for | Cognitive Function |
|---|------|------|-------|----------|-------------------|
| P1 | **Auditor** | 0.02 | 0.55 | security audit, vulnerability scan | Nearly deterministic + narrow vocabulary. Finds the worst possible interpretation of every statement. Excels at: edge cases, failure modes, threat modeling. |
| P2 | **Judge** | 0.08 | 0.75 | correctness verification, fact-checking | Highly precise. Verifies claims against evidence. Excels at: logical consistency, contradiction detection, factual accuracy. |
| P3 | **Surgeon** | 0.12 | 0.88 | refactoring, bug fixing, code review | Precise but with surgical vocabulary breadth. Excels at: pinpointing exact issues, suggesting minimal fixes. |

### Structured Band (0.15–0.35 temp)

| # | Role | temp | top_p | Best for | Cognitive Function |
|---|------|------|-------|----------|-------------------|
| S1 | **Architect** | 0.20 | 0.82 | system design, architecture planning | Structured, methodical thinking. Excels at: modular design, API contracts, data modeling. |
| S2 | **Clarifier** | 0.25 | 0.92 | documentation, explanation, teaching | Clear, precise language with broad vocabulary. Excels at: making complex ideas simple, writing docs. |
| S3 | **Skeptic** | 0.30 | 0.80 | critique, review, adversarial testing | Focused, questioning. Excels at: finding flaws in arguments, identifying hidden assumptions. |
| S4 | **Scholar** | 0.35 | 0.85 | literature review, research, analysis | Balanced academic. Excels at: systematic evaluation, comparative analysis, taxonomy. |

### Creative Band (0.40–0.70 temp)

| # | Role | temp | top_p | Best for | Cognitive Function |
|---|------|------|-------|----------|-------------------|
| C1 | **Diplomat** | 0.40 | 0.88 | trade-off analysis, decision-making | Balanced + nuanced. Excels at: weighing options, finding compromises, multi-stakeholder reasoning. |
| C2 | **Teacher** | 0.50 | 0.90 | onboarding, tutorials, examples | Accessible + broad. Excels at: step-by-step reasoning, analogies, worked examples. |
| C3 | **Optimizer** | 0.55 | 0.90 | performance tuning, optimization | Systematic improvement. Excels at: finding bottlenecks, proposing incremental gains. |
| C4 | **Synthesizer** | 0.45 | 0.92 | pattern recognition, summarization | Connects disparate ideas. Excels at: finding common themes, meta-analysis. |
| C5 | **Explorer** | 0.60 | 0.92 | brainstorming, ideation, greenfield | Balanced creativity. Excels at: generating alternatives, divergent thinking with grounding. |

### Divergent Band (0.70–1.20 temp)

| # | Role | temp | top_p | Best for | Cognitive Function |
|---|------|------|-------|----------|-------------------|
| D1 | **Innovator** | 0.70 | 0.95 | novel solutions, breakthrough ideas | Creative + wide. Excels at: unconventional approaches, paradigm shifts. |
| D2 | **Visionary** | 0.85 | 0.98 | long-term strategy, futuristic thinking | Far-sighted. Excels at: trend prediction, scenario planning, moonshots. |
| D3 | **Brainstormer** | 0.90 | 0.95 | rapid ideation, quantity over quality | Maximum useful creativity. Excels at: generating many ideas quickly, lateral thinking. |
| D4 | **Provocateur** | 1.20 | 0.98 | challenging assumptions, contrarian views | Deliberately unconventional. Excels at: breaking groupthink, questioning axioms. |

---

## Query Type → Role Selection Guide

When a debate query arrives, select roles based on the query's nature:

| Query Type | Mandatory | Recommended Roles | Total Sessions |
|-----------|-----------|-------------------|----------------|
| **Audit / Security** | Oracle, Chaos | Auditor, Judge, Skeptic | (2+3)×3 = 15 |
| **Design / Architecture** | Oracle, Chaos | Architect, Diplomat, Explorer | (2+3)×3 = 15 |
| **Code / Implementation** | Oracle, Chaos | Surgeon, Architect, Optimizer | (2+3)×3 = 15 |
| **Research / Analysis** | Oracle, Chaos | Scholar, Skeptic, Synthesizer | (2+3)×3 = 15 |
| **Creative / Ideation** | Oracle, Chaos | Innovator, Brainstormer, Visionary | (2+3)×3 = 15 |
| **Review / Critique** | Oracle, Chaos | Judge, Skeptic, Clarifier | (2+3)×3 = 15 |
| **Documentation / Teaching** | Oracle, Chaos | Clarifier, Teacher, Scholar | (2+3)×3 = 15 |
| **Trade-off / Decision** | Oracle, Chaos | Diplomat, Skeptic, Optimizer | (2+3)×3 = 15 |
| **Deep (comprehensive)** | Oracle, Chaos | All 18 roles | (2+18)×3 = 60 |

---

## Why Oracle and Chaos Are Always Included

```
Oracle (0,0) ──────────────────────────────────── Chaos (2,1)
    ↑                                                    ↑
    "best possible determinism"              "maximum possible randomness"
    lower bound on precision                 upper bound on creativity
    if Oracle agrees → highly confident      if Chaos produces value → trivial problem
```

1. **Calibration**: Oracle and Chaos bracket the solution space. Every useful answer lies between them.
2. **Baseline**: If a proposal is worse than Oracle's output (which you'd expect to be "correct but boring"), it's flawed. If a proposal is less creative than Chaos (which you'd expect to be "creative but incoherent"), why use LLM at all?
3. **Consensus signal**: When Oracle AND Chaos agree on something, it's a strong signal. Oracle agreeing = it's deterministic. Chaos agreeing = it's so obvious that even random sampling gets it right.

---

## Role Pool Summary

```
top_p
1.00 ┤                                              × Chaos (2.0, 1.0)
0.95 ┤                        × Inn  × Vis  × Brs
0.90 ┤        × Clr  × Tch  × Opt  × Syn  × Exp
0.85 ┤     × Sch  × Dip                             
0.80 ┤  × Jdg     × Ske × Arc
0.75 ┤  × Jdg                                         
0.70 ┤
0.55 ┤  × Aud
0.00 ┤  × Oracle (0.0, 0.0)
     └──────────────────────────────────────────────── temp
      0.0  0.1  0.2  0.3  0.4  0.5  0.6  0.7  0.9  1.2  2.0

Anchor roles: Oracle (min) + Chaos (max) — always included
Pool roles: 18 roles in 4 bands — selected per query type
Each role: 3 sessions (correctness + simplicity + performance perspectives)
```

---

## Proxy Config Integration

Each role's (temp, top_p) is written to `proxy-config.json` with 3 session entries:

```json
{
  "s1":  {"temperature": 0.0,  "top_p": 0.0,  "role": "Oracle",       "perspective": "correctness"},
  "s2":  {"temperature": 0.0,  "top_p": 0.0,  "role": "Oracle",       "perspective": "simplicity"},
  "s3":  {"temperature": 0.0,  "top_p": 0.0,  "role": "Oracle",       "perspective": "performance"},
  "s4":  {"temperature": 0.02, "top_p": 0.55, "role": "Auditor",      "perspective": "correctness"},
  "s5":  {"temperature": 0.02, "top_p": 0.55, "role": "Auditor",      "perspective": "simplicity"},
  "s6":  {"temperature": 0.02, "top_p": 0.55, "role": "Auditor",      "perspective": "performance"},
  ...
  "s58": {"temperature": 2.0,  "top_p": 1.00, "role": "Chaos",        "perspective": "correctness"},
  "s59": {"temperature": 2.0,  "top_p": 1.00, "role": "Chaos",        "perspective": "simplicity"},
  "s60": {"temperature": 2.0,  "top_p": 1.00, "role": "Chaos",        "perspective": "performance"}
}
```
