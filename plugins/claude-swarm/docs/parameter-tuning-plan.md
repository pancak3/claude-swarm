# Swarm Parameter Tuning — Metaheuristic Approach

Grounded in *Essentials of Metaheuristics* (Sean Luke, 2015).  
Task: select (temperature, top_p) pairs for N sessions to maximize debate efficiency.

---

## 1. The Parameter Space

```
top_p (y-axis)
1.0 ┤  ┌─────────────┬─────────────┬─────────────┬─────────────┐
    │  │  EXPLORE    │  BRAINSTORM │  DIVERGE    │  WANDER     │
0.9 ┤  │  (cold/wide)│ (hot/wide)  │ (hotter)    │ (random)    │
    │  ├─────────────┼─────────────┼─────────────┼─────────────┤
0.8 ┤  │  FOCUS      │  BALANCED   │  CREATIVE   │  CHAOTIC    │
    │  │  (cold/nar) │ (moderate)  │ (hot/mid)   │ (too hot)   │
    │  ├─────────────┼─────────────┼─────────────┼─────────────┤
0.7 ┤  │  PRECISE    │  SAFE       │  RISKY      │  INCOHERENT │
    │  │  (v cold)   │ (mod/nar)   │ (hot/nar)   │ (max rand)  │
    │  └─────────────┴─────────────┴─────────────┴─────────────┘
        0.0    0.3    0.6    0.9    1.2    1.5    1.8    2.0
                                  temperature (x-axis)
```

Two dimensions, four meaningful regimes:

| Regime | temp range | top_p range | Cognitive style |
|--------|-----------|-------------|-----------------|
| **Exploit** | 0.0–0.2 | 0.7–0.85 | Precise, deterministic, thorough |
| **Focus** | 0.2–0.4 | 0.8–0.9 | Structured, logical, clear |
| **Balance** | 0.4–0.7 | 0.85–0.95 | Creative but grounded |
| **Explore** | 0.7–1.0 | 0.9–1.0 | Divergent, innovative, risk-taking |

---

## 2. Metaheuristic → Swarm Mapping

### 2.1 Simulated Annealing (SA)

SA starts hot (high temp → high randomness, exploration), then cools (lower temp → exploitation). The cooling schedule determines how many steps at each temperature.

**SA → Swarm mapping**: Instead of a temporal schedule (one session cooling over time), we spatialize it: assign different sessions to different points on the cooling curve. This gives us a **population snapshot** of the entire annealing trajectory in parallel.

Sessions 1–2: Hot (temp 0.7–1.0) — broad exploration, generate diverse proposals  
Sessions 3–5: Warm (temp 0.3–0.6) — structured creativity, refine ideas  
Sessions 6–8: Cold (temp 0.05–0.2) — precision, verify correctness, find flaws

The debate rounds naturally align: hot sessions dominate PROPOSE (explore), warm sessions dominate CRITIQUE (evaluate), cold sessions dominate VOTE (exploit).

### 2.2 N-Population Cooperative Coevolution (CCEA)

In CCEA, N subpopulations each evolve a piece of a decomposed solution. They cooperate by sharing their best individuals.

**CCEA → Swarm mapping**: Each perspective (correctness/simplicity/performance/security) is a subpopulation. Within each perspective, we use 2 temperature variants:

- One cold variant: exploit what's known, verify correctness within the perspective
- One hot variant: explore new ideas, push the boundary of the perspective

This gives us 4 × 2 = 8 sessions with deliberate parameter diversity.

### 2.3 Differential Evolution (DE)

DE creates new candidate vectors by adding weighted differences between existing vectors: `new = A + F × (B − C)`.

**DE → Swarm mapping**: The "difference" between a hot session and a cold session within the same perspective is the creative differential. When they cross-critique, the hot session's proposal challenges the cold session's assumptions, and the cold session's precision tempers the hot session's creativity.

### 2.4 Ant Colony Optimization (ACO)

ACO uses pheromone trails — ants that find good paths deposit pheromone, attracting other ants. Over time, paths converge.

**ACO → Swarm mapping**: The vote tally is the pheromone trail. Sessions vote on proposals; winning proposals attract more votes in runoff rounds. The parameter diversity ensures the "ants" explore different regions before converging.

---

## 3. Recommended Parameter Schedule for 8 Sessions

### Design principle: Sample along the Pareto frontier of exploration vs exploitation

We want to **cover the 2D space efficiently** — not random, not uniform grid. Sample along the diagonal from (cold, narrow) to (hot, wide), with deliberate off-diagonal variants.

| # | Perspective | temp | top_p | Nickname | Cognitive Role |
|---|-------------|------|-------|----------|----------------|
| s1 | correctness | 0.05 | 0.70 | **The Judge** | Ultra-precise, verifies every claim |
| s2 | simplicity | 0.15 | 0.88 | **The Clarifier** | Clear, precise explanations |
| s3 | performance | 0.80 | 0.95 | **The Inventor** | Creative, generates novel solutions |
| s4 | security | 0.02 | 0.60 | **The Auditor** | Most conservative, finds worst-case flaws |
| s5 | correctness | 0.35 | 0.82 | **The Architect** | Structured design, balanced |
| s6 | simplicity | 0.50 | 0.90 | **The Teacher** | Accessible, broad vocabulary |
| s7 | performance | 0.60 | 0.92 | **The Optimizer** | Refines, improves |
| s8 | security | 0.10 | 0.75 | **The Guardian** | Conservative, checks boundaries |

### Visualized in parameter space

```
top_p
0.95 ┤                          × s3 (Inventor)      ← exploration ceiling
0.90 ┤            × s6 (Teacher)  × s7 (Optimizer)
0.85 ┤     × s2 (Clarifier)                           
0.80 ┤              × s5 (Architect)                  ← balanced band
0.75 ┤  × s8 (Guardian)
0.70 ┤  × s1 (Judge)                                  ← precision band
0.60 ┤  × s4 (Auditor)                                ← exploitation floor
     └─────────────────────────────────────────
       0.02  0.15  0.35  0.50  0.60  0.80          temp
         cold ←────────────────────────→ hot
```

Coverage properties:
- **temp range**: 0.02–0.80 (entire useful range; >0.9 risks incoherence)
- **top_p range**: 0.60–0.95 (avoids <0.6 which truncates too aggressively)
- **No two sessions share the same (temp, top_p)** — every pair is unique
- **Density higher in the "balanced band"** (0.15–0.60 temp, 0.80–0.92 top_p) — most useful region

---

## 4. Integration via SWARM_PROXY_PARAMS

```bash
SWARM_PROXY_PARAMS='[
  {"perspective":"correctness","temperature":0.05,"top_p":0.70},
  {"perspective":"simplicity","temperature":0.15,"top_p":0.88},
  {"perspective":"performance","temperature":0.80,"top_p":0.95},
  {"perspective":"security","temperature":0.02,"top_p":0.60},
  {"perspective":"correctness","temperature":0.35,"top_p":0.82},
  {"perspective":"simplicity","temperature":0.50,"top_p":0.90},
  {"perspective":"performance","temperature":0.60,"top_p":0.92},
  {"perspective":"security","temperature":0.10,"top_p":0.75}
]'
```

The orchestrator's proxy config generator already supports this — it reads SWARM_PROXY_PARAMS, merges matching perspective entries into defaults, and assigns session i to `perspectives[i%4]` with the merged params.

---

## 5. Why This Beats Random Assignment

| Approach | Property | Problem |
|----------|----------|---------|
| All same temp | Zero diversity | Sessions are clones — waste of N sessions |
| Random temp | Maximum entropy | May sample useless regions (temp>1.0 = incoherent; top_p<0.5 = truncated) |
| Uniform grid | Even coverage | Wastes sessions on obviously bad regions |
| **Metaheuristic frontier** | Strategic coverage | Dense in useful region, sparse in bad regions, covers extremes |

The metaheuristic approach samples the **Pareto-optimal frontier** of the (exploration, exploitation) tradeoff — every session has a unique and useful role.

---

## 6. Future: Adaptive Parameter Selection

A natural extension: use the vote tally from round 1 to ADAPT session parameters for round 2+. If votes are degenerate (all self-votes, Gini≈0), increase temperature diversity. If votes are highly concentrated on one proposal, reduce temperature to verify it thoroughly. This is essentially a **Simulated Annealing cooling schedule driven by real-time swarm feedback** — but that's v1.0.
