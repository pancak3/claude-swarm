#!/usr/bin/env python3
"""swarm-summarize.py — lightweight meta-synthesizer for swarm session conclusions.

Reads session log files and produces a structured summary:
  - Key findings: phrases mentioned by N+ sessions
  - Controversial items: phrases with strong pro/con distribution
  - Action items: imperative sentences from conclusions

Usage:
  python3 swarm-summarize.py <rundir> [--threshold N] [--output FILE]
"""

import argparse
import json
import os
import re
import sys
from collections import Counter, defaultdict


def extract_conclusions(log_path: str) -> str:
    """Extract the last meaningful paragraph from a session log."""
    if not os.path.isfile(log_path):
        return ""
    paragraphs = []
    current = []
    with open(log_path, "r", errors="replace") as f:
        for line in f:
            stripped = line.strip()
            if not stripped:
                if current:
                    paragraphs.append(" ".join(current))
                    current = []
            else:
                current.append(stripped)
        if current:
            paragraphs.append(" ".join(current))
    # Find last paragraph that's substantial
    for para in reversed(paragraphs):
        if len(para) >= 80:
            return para
    return paragraphs[-1] if paragraphs else ""


# Key technical phrases to track (configurable)
DEFAULT_KEY_PHRASES = [
    # Architecture
    "microservice", "monolith", "event-driven", "cqrs", "event sourcing",
    "rest api", "graphql", "grpc", "message queue", "pub/sub",
    # Data
    "database", "cache", "index", "sharding", "replication",
    "consistency", "availability", "partition tolerance", "acid", "base",
    # Performance
    "latency", "throughput", "scalability", "concurrency", "parallelism",
    "load balancing", "rate limiting", "backpressure", "circuit breaker",
    # Security
    "authentication", "authorization", "encryption", "audit log",
    "input validation", "sanitization", "zero trust",
    # Quality
    "test coverage", "unit test", "integration test", "error handling",
    "logging", "monitoring", "observability", "tracing", "alerting",
    # Process
    "migration", "refactoring", "deprecation", "rollback",
    "documentation", "code review", "technical debt",
    # Deployment
    "ci/cd", "containerization", "orchestration", "blue-green",
    "canary", "feature flag", "infrastructure as code",
]


def extract_phrases(text: str, phrases: list) -> Counter:
    """Count occurrences of key phrases in text (case-insensitive)."""
    text_lower = text.lower()
    counts = Counter()
    for phrase in phrases:
        pattern = re.compile(re.escape(phrase.lower()))
        matches = pattern.findall(text_lower)
        if matches:
            counts[phrase] = len(matches)
    return counts


def extract_action_items(text: str) -> list:
    """Extract imperative sentences that look like action items."""
    items = []
    # Look for lines starting with action verbs, or sentences with "should"/"must"/"need to"
    lines = text.split("\n")
    for line in lines:
        line = line.strip()
        if not line:
            continue
        # Bullet points starting with action verbs
        if re.match(r"^[-*]\s+(Implement|Add|Create|Fix|Refactor|Write|Update|Remove|Migrate|Test|Deploy|Document|Configure|Set up|Build|Optimize)\b", line, re.IGNORECASE):
            items.append(line.lstrip("-* ").strip())
        # Sentences with modal verbs indicating action
        for match in re.finditer(r"(?:we\s+)?(should|must|need to|have to|ought to)\s+([^.]*\.)", line, re.IGNORECASE):
            full = match.group(0).strip()
            if full not in items:
                items.append(full)
    return items


def compute_agreement(phrase_sessions: dict, total_sessions: int, min_share: float = 0.3) -> list:
    """Compute agreement levels: what fraction of sessions mentioned each phrase."""
    results = []
    for phrase, sessions in phrase_sessions.items():
        count = len(sessions)
        share = count / total_sessions if total_sessions > 0 else 0
        if share >= min_share:
            results.append({
                "phrase": phrase,
                "sessions": count,
                "total_sessions": total_sessions,
                "share": round(share, 2),
            })
    results.sort(key=lambda x: -x["share"])
    return results


def main():
    parser = argparse.ArgumentParser(description="Swarm output summarization")
    parser.add_argument("rundir", help="Swarm run directory (containing s1.log, s2.log, ...)")
    parser.add_argument("--threshold", type=int, default=2,
                        help="Minimum sessions mentioning a phrase for it to be a key finding (default: 2)")
    parser.add_argument("--output", "-o", help="Output file (default: stdout)")
    parser.add_argument("--phrases", nargs="*", default=DEFAULT_KEY_PHRASES,
                        help="Key phrases to track (default: technical phrases)")
    args = parser.parse_args()

    # Discover session logs
    log_files = sorted([
        os.path.join(args.rundir, f)
        for f in os.listdir(args.rundir)
        if re.match(r"s\d+\.log$", f)
    ])

    if not log_files:
        print(json.dumps({"error": "No session logs found"}, indent=2))
        sys.exit(1)

    # Extract conclusions and count phrases
    session_data = {}
    total = len(log_files)
    phrase_sessions = defaultdict(set)  # phrase -> set of session IDs
    all_phrases = Counter()
    all_action_items = []

    for lf in log_files:
        sid = re.match(r"(s\d+)", os.path.basename(lf))
        sid = sid.group(1) if sid else os.path.basename(lf)
        conclusion = extract_conclusions(lf)
        session_data[sid] = conclusion

        if not conclusion:
            continue

        phrases = extract_phrases(conclusion, args.phrases)
        all_phrases.update(phrases)
        for phrase in phrases:
            phrase_sessions[phrase].add(sid)

        actions = extract_action_items(conclusion)
        all_action_items.extend(actions)

    # Build structured output
    key_findings = compute_agreement(phrase_sessions, total, min_share=args.threshold / total)

    # Controversial items: phrases mentioned by at least 2 sessions but with
    # significant variation in context (simplified: mentioned but not universal agreement)
    controversial = [f for f in key_findings if 0.2 <= f["share"] <= 0.8]

    summary = {
        "swarm_summary": {
            "total_sessions": total,
            "sessions_analyzed": len(session_data),
        },
        "key_findings": key_findings,
        "controversial_items": [c["phrase"] for c in controversial],
        "action_items": all_action_items[:20],  # limit to top 20
        "top_phrases": all_phrases.most_common(15),
    }

    output = json.dumps(summary, indent=2)
    if args.output:
        with open(args.output, "w") as f:
            f.write(output + "\n")
        print(f"Summary written to {args.output}")
    else:
        print(output)


if __name__ == "__main__":
    main()
