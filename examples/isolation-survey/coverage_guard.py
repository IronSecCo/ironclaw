#!/usr/bin/env python3
"""coverage_guard.py — fail a survey run that lost coverage it used to have.

`survey.sh` is deliberately tolerant of a scenario it cannot pull, run or scan:
it is sweeping 295 images from live public registries, and one transient 500
should never wedge the weekly scorecard refresh. The bug (IRO-727) was that the
only guard on the whole sweep was `scanned > 0`, so 39 rows failed every single
week and the run still exited green with a clean-looking artifact.

"Any skip is fatal" is the wrong correction — it trades a silent hole for a
pipeline that goes red on registry weather, and a guard you learn to re-run on
red is not a guard. So this checks for **regression**, not absence:

* Every scenario that produced output in the PREVIOUS results.json and is STILL
  listed in the manifest must produce output again. A genuinely flaky pull is
  missing from the baseline too, so it cannot trip this; a row that fails
  deterministically week after week is invisible to it only until the first week
  it succeeds, after which it is pinned.
* A row deliberately deleted from images.txt is not a regression — retiring a
  dead row is the intended fix, so the baseline is intersected with the current
  manifest.
* The absolute floor is the backstop, and it comes from the previous run's
  count, not from 1. With no baseline available (a first run, or a fresh
  checkout with no committed results.json) it degrades to "> 0" and says so,
  loudly, instead of pretending it verified something.

Usage:

    coverage_guard.py --manifest images.txt --results results.json \
                      [--baseline previous-results.json] [--min-scanned N]

Exits 0 when coverage held, 1 on a coverage regression, 2 on a usage/IO error.
Negative controls: scripts/tests/test_coverage_guard.py.
"""

from __future__ import annotations

import argparse
import json
import sys


def manifest_labels(path):
    """Scenario labels in images.txt, in file order, deduplicated.

    Mirrors survey.sh's own parse: strip CR, skip blanks and `#` comments, split
    on `|`, take field 1. If these two ever disagree the guard measures a
    different manifest than the sweep did.
    """
    labels = []
    seen = set()
    with open(path) as f:
        for line in f:
            line = line.strip().rstrip("\r")
            if not line or line.startswith("#"):
                continue
            label = line.split("|")[0].strip()
            if not label or label in seen:
                continue
            seen.add(label)
            labels.append(label)
    return labels


def scenario_labels(results_path):
    """Labels that produced a scorecard in a results.json."""
    with open(results_path) as f:
        doc = json.load(f)
    return {s.get("label", "") for s in doc.get("scenarios", [])}


def skip_index(results_path):
    """label -> {stage, reason} for the scenarios this run recorded as skipped.

    Empty for a schema-1.0 results.json, which had nowhere to record them; the
    guard still works, it just cannot explain the regression it found.
    """
    with open(results_path) as f:
        doc = json.load(f)
    return {s.get("label", ""): s for s in doc.get("skipped", [])}


def evaluate(manifest, scanned, baseline, min_scanned=None):
    """Decide whether coverage held.

    manifest -- labels the manifest asked for (list, file order)
    scanned  -- labels that produced a scorecard this run (set)
    baseline -- labels that produced one last run, or None when unavailable
    Returns (ok, floor, regressed, messages).
    """
    messages = []
    regressed = []

    if baseline is None:
        expected = None
        floor = 1 if min_scanned is None else min_scanned
        messages.append(
            "no baseline results.json: coverage regression is UNCHECKED this "
            f"run, falling back to a floor of {floor}")
    else:
        # Only rows the manifest still asks for. Deleting a dead row is the
        # intended fix for a permanently failing scenario, not a regression.
        expected = [lab for lab in manifest if lab in baseline]
        regressed = [lab for lab in expected if lab not in scanned]
        floor = len(expected) if min_scanned is None else min_scanned

    ok = not regressed and len(scanned) >= floor
    return ok, floor, regressed, messages


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--results", required=True,
                    help="results.json this run just wrote")
    ap.add_argument("--baseline", default="",
                    help="the previous results.json, captured before the run "
                         "overwrote it")
    ap.add_argument("--min-scanned", type=int, default=None,
                    help="override the absolute floor (default: the baseline's "
                         "own count)")
    args = ap.parse_args(argv)

    try:
        manifest = manifest_labels(args.manifest)
        scanned = scenario_labels(args.results)
        skips = skip_index(args.results)
        baseline = scenario_labels(args.baseline) if args.baseline else None
    except (OSError, ValueError, KeyError) as exc:
        print(f"coverage guard: cannot read inputs: {exc}", file=sys.stderr)
        return 2

    ok, floor, regressed, messages = evaluate(
        manifest, scanned, baseline, args.min_scanned)

    print(f"coverage: {len(scanned)} scanned / {len(manifest)} manifest rows "
          f"/ {len(skips)} recorded skips (floor {floor})", file=sys.stderr)
    for m in messages:
        print(f"coverage guard: {m}", file=sys.stderr)

    if regressed:
        print(f"coverage guard: FAIL — {len(regressed)} scenario(s) scored in "
              "the previous results.json and produced nothing now:",
              file=sys.stderr)
        for lab in regressed:
            s = skips.get(lab)
            why = (f"failed at {s.get('stage', '?')}: "
                   f"{(s.get('reason') or '').strip()}") if s else \
                "never reached the sweep (not attempted, or the run died early)"
            print(f"  - {lab}: {why}", file=sys.stderr)
        print("coverage guard: fix the rows, or delete them from the manifest "
              "if they can never be scanned again.", file=sys.stderr)
    elif len(scanned) < floor:
        print(f"coverage guard: FAIL — only {len(scanned)} scenarios scanned, "
              f"floor is {floor}.", file=sys.stderr)

    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
