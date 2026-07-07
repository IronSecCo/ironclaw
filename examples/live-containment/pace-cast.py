#!/usr/bin/env python3
"""Pace the live-containment asciinema cast for a legible README hero.

run.sh streams its whole transcript in well under two seconds, so a raw
recording renders as an unreadable text-dump (a handful of near-identical
GIF frames). This script keeps the terminal OUTPUT byte-for-byte from the
real `--attach` run and only re-times the inter-line reveal so each
containment step holds long enough to read. It never invents, edits, or
stages a single character of output; input timing is discarded, so running
it on an already-paced cast reproduces the same result (idempotent).

Usage:
    examples/live-containment/pace-cast.py RAW.cast > docs/assets/live-containment.cast
"""
import json
import re
import sys

ANSI = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]")


def dwell(line: str) -> float:
    """Seconds to hold after emitting `line`, keyed off its rendered text."""
    t = ANSI.sub("", line)
    if "CONTAINMENT SUMMARY" in t:
        return 1.8
    if "BLOCKED" in t or "⛔" in t:      # hold each verdict long enough to read
        return 1.5
    if re.search(r"\[[123]/3\]", t):          # attempt header
        return 0.85
    if "engaging a live sandbox" in t:
        return 0.8
    if "sandbox under test" in t:
        return 0.9
    if "isolation you can prove" in t:
        return 1.1
    if t.strip() == "":
        return 0.09
    if t.strip().startswith("==="):
        return 0.04
    return 0.22                                # normal narrative line


def main() -> int:
    if len(sys.argv) != 2:
        sys.stderr.write(__doc__)
        return 2
    raw = open(sys.argv[1]).read().splitlines()
    src = json.loads(raw[0])
    term = src.get("term", {})
    cols = term.get("cols", src.get("width", 92))
    rows = term.get("rows", src.get("height", 28))

    output = []
    for ln in raw[1:]:
        ln = ln.strip()
        if not ln:
            continue
        ev = json.loads(ln)
        if len(ev) >= 3 and ev[1] == "o":
            output.append(ev[2])
    full = "".join(output)

    parts = full.split("\n")
    lines = [p + "\n" for p in parts[:-1]] + ([parts[-1]] if parts[-1] else [])

    events = []
    t = 0.3
    for ln in lines:
        events.append([round(t, 3), "o", ln])
        t += dwell(ln)

    header = {
        "version": 2,
        "width": cols,
        "height": rows,
        "title": "IronClaw live containment — a jailbroken agent tries to escape, the box holds",
        "env": {"SHELL": "/bin/zsh"},
    }
    out = sys.stdout
    out.write(json.dumps(header) + "\n")
    for e in events:
        out.write(json.dumps(e, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
