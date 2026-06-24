#!/usr/bin/env python3
"""Lightweight meta-synthesizer for swarm debate output.

Reads session output directories, does frequency counting of key phrases,
and produces a structured summary of:
- Key findings (phrases N sessions agree on)
- Controversial items (phrases with mixed framing)
- Action items (imperative phrases)

No external dependencies -- uses Python stdlib only.
"""

import argparse
import json
import os
import re
import sys
from collections import Counter
from pathlib import Path


# Phrases indicating agreement or key findings
AGREEMENT_INDICATORS = [
    "should", "must", "need to", "important", "critical", "key",
    "recommend", "suggest", "propose", "agree", "consensus",
    "every session", "all sessions", "unanimous",
]

# Phrases indicating controversy or disagreement
CONTROVERSY_INDICATORS = [
    "however", "but", "although", "disagree", "disagreement",
    "concern", "concerns", "controversial", "debate", "dispute",
    "on the other hand", "alternative", "option", "tradeoff",
    "trade-off", "risk", "risks", "limitation", "limitations",
]

# Action-oriented phrases
ACTION_INDICATORS = [
    "implement", "create", "add", "remove", "refactor", "update",
    "fix", "change", "replace", "migrate", "test", "verify",
    "document", "review", "audit", "optimize", "improve",
]


def normalize_phrase(text: str) -> str:
    """Normalize a phrase for comparison."""
    text = text.lower().strip()
    text = re.sub(r'[^\w\s]', '', text)
    text = re.sub(r'\s+', ' ', text)
    return text


def extract_bigrams(text: str) -> list:
    """Extract meaningful bigrams from text."""
    words = re.findall(r'\b[a-zA-Z]{3,}\b', text.lower())
    bigrams = []
    for i in range(len(words) - 1):
        bigrams.append(f"{words[i]} {words[i+1]}")
    return bigrams


def extract_trigrams(text: str) -> list:
    """Extract meaningful trigrams from text."""
    words = re.findall(r'\b[a-zA-Z]{3,}\b', text.lower())
    trigrams = []
    for i in range(len(words) - 2):
        trigrams.append(f"{words[i]} {words[i+1]} {words[i+2]}")
    return trigrams


def find_action_items(text: str) -> list:
    """Find imperative/action sentences."""
    sentences = re.split(r'[.!?]+', text)
    actions = []
    for s in sentences:
        s = s.strip()
        if not s:
            continue
        s_lower = s.lower()
        # Check if starts with action indicator or contains action phrases
        for indicator in ACTION_INDICATORS:
            if indicator in s_lower:
                actions.append(s[:120].strip())
                break
    return actions


def find_controversial_phrases(text: str) -> list:
    """Find sentences containing controversy indicators."""
    sentences = re.split(r'[.!?]+', text)
    controversial = []
    for s in sentences:
        s = s.strip()
        if not s:
            continue
        s_lower = s.lower()
        for indicator in CONTROVERSY_INDICATORS:
            if indicator in s_lower:
                controversial.append(s[:120].strip())
                break
    return controversial


def parse_session_outputs(run_dir: str) -> dict:
    """Read all session output files from the run directory."""
    run_path = Path(run_dir)
    sessions_dir = run_path / "sessions"

    if not sessions_dir.exists():
        # Try direct session log files
        texts = {}
        for f in sorted(run_path.glob("s*.log")):
            text = f.read_text(errors="replace")
            if text.strip():
                texts[f.stem] = text
        return texts

    texts = {}
    for session_dir in sorted(sessions_dir.iterdir()):
        if not session_dir.is_dir():
            continue
        output_dir = session_dir / "output"
        if not output_dir.exists():
            continue
        session_texts = []
        for f in output_dir.iterdir():
            if f.is_file() and f.suffix in {".md", ".txt", ".log", ".json"}:
                try:
                    session_texts.append(f.read_text(errors="replace"))
                except Exception:
                    pass
        if session_texts:
            texts[session_dir.name] = "\n".join(session_texts)

    return texts


def synthesize(texts: dict) -> dict:
    """Produce structured summary from session texts."""
    all_text = " ".join(texts.values())
    session_count = len(texts)

    # Frequency counting of bigrams and trigrams
    all_bigrams = Counter()
    all_trigrams = Counter()
    per_session_bigrams = {}
    per_session_trigrams = {}

    for sid, text in texts.items():
        bigrams = extract_bigrams(text)
        trigrams = extract_trigrams(text)
        per_session_bigrams[sid] = set(bigrams)
        per_session_trigrams[sid] = set(trigrams)
        all_bigrams.update(bigrams)
        all_trigrams.update(trigrams)

    # Find key findings: bigrams appearing in >50% of sessions
    threshold = max(2, session_count // 2)
    agreement_phrases = []
    for phrase, count in all_bigrams.most_common(50):
        occurrence = sum(1 for sgrams in per_session_bigrams.values() if phrase in sgrams)
        if occurrence >= threshold and count >= 2:
            agreement_phrases.append((phrase, occurrence, count))

    # Find agreement trigrams
    agreement_trigrams = []
    for phrase, count in all_trigrams.most_common(50):
        occurrence = sum(1 for sgrams in per_session_trigrams.values() if phrase in sgrams)
        if occurrence >= threshold and count >= 2:
            agreement_trigrams.append((phrase, occurrence, count))

    # Extract controversial items from each session
    all_controversial = Counter()
    for text in texts.values():
        for item in find_controversial_phrases(text):
            all_controversial[normalize_phrase(item)] += 1

    # Extract action items
    all_actions = Counter()
    for text in texts.values():
        for action in find_action_items(text):
            all_actions[normalize_phrase(action)] += 1

    # Build structured output
    result = {
        "session_count": session_count,
        "key_findings": [
            {
                "phrase": p,
                "sessions": c,
                "occurrences": o,
            }
            for p, c, o in agreement_phrases[:15]
        ],
        "agreement_phrases": [
            {
                "phrase": p,
                "sessions": c,
            }
            for p, c, _ in agreement_phrases[:10]
        ],
        "controversial_items": [
            {"topic": phrase, "mentions": count}
            for phrase, count in all_controversial.most_common(10)
        ],
        "action_items": [
            {"action": phrase, "mentions": count}
            for phrase, count in all_actions.most_common(15)
        ],
    }

    return result


def format_summary(result: dict, task: str = "") -> str:
    """Format structured summary as human-readable text."""
    lines = []
    lines.append("=" * 68)
    lines.append("  SWARM SYNTHESIS SUMMARY")
    if task:
        lines.append(f"  Task: {task}")
    lines.append(f"  Sessions analyzed: {result['session_count']}")
    lines.append("=" * 68)
    lines.append("")

    # Key findings
    lines.append("--- Key Findings (cross-session agreement) ---")
    if result["key_findings"]:
        for f in result["key_findings"][:10]:
            lines.append(f"  [{f['sessions']}/{result['session_count']}] {f['phrase']}")
    else:
        lines.append("  (no strong cross-session agreement detected)")
    lines.append("")

    # Controversial items
    lines.append("--- Controversial Items ---")
    if result["controversial_items"]:
        for item in result["controversial_items"][:8]:
            lines.append(f"  [{item['mentions']} mentions] {item['topic']}")
    else:
        lines.append("  (no controversial items detected)")
    lines.append("")

    # Action items
    lines.append("--- Action Items ---")
    if result["action_items"]:
        for item in result["action_items"][:10]:
            lines.append(f"  [priority] {item['action']}")
    else:
        lines.append("  (no action items detected)")
    lines.append("")

    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(
        description="Lightweight swarm debate summarizer"
    )
    parser.add_argument(
        "run_dir",
        help="Path to swarm run directory containing sessions/",
    )
    parser.add_argument(
        "--format",
        choices=["text", "json"],
        default="text",
        help="Output format",
    )
    parser.add_argument(
        "--task",
        default="",
        help="Task description (for display)",
    )
    args = parser.parse_args()

    texts = parse_session_outputs(args.run_dir)
    if not texts:
        print("ERROR: No session output files found", file=sys.stderr)
        sys.exit(1)

    result = synthesize(texts)

    if args.format == "json":
        print(json.dumps(result, indent=2))
    else:
        print(format_summary(result, task=args.task))


if __name__ == "__main__":
    main()
