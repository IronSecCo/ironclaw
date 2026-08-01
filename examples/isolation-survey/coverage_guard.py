#!/usr/bin/env python3
r"""coverage_guard.py — fail a survey run that lost coverage it used to have.

`survey.sh` is deliberately tolerant of a scenario it cannot pull, run or scan:
it is sweeping 295 images from live public registries, and one transient 500
should never wedge the weekly scorecard refresh. The bug (IRO-727) was that the
only guard on the whole sweep was `scanned > 0`, so 39 rows failed every single
week and the run still exited green with a clean-looking artifact.

"Any skip is fatal" is the wrong correction — it trades a silent hole for a
pipeline that goes red on registry weather, and a guard you learn to re-run on
red is not a guard. So this checks two things instead:

1. **No regression.** Every scenario that produced output in the baseline
   results.json and is STILL listed in the manifest must produce output again. A
   genuinely flaky pull is missing from the baseline too, so it cannot trip
   this; a row that fails deterministically week after week is invisible to it
   only until the first week it succeeds, after which it is pinned. A row
   deliberately deleted from images.txt is not a regression — retiring a dead
   row is the intended fix, so the baseline is intersected with the current
   manifest.

   The baseline must be the last COMMITTED results.json, never the working-tree
   copy the run is about to overwrite: see the header of `survey.sh`. Taking it
   from disk lets a plain re-run launder a regression green.

2. **The accounting adds up.** `scenarioCount + skippedCount == manifestRowCount
   == the rows this guard parses out of images.txt`. That is the invariant the
   artifact advertises, and it is what makes "256 scenarios" readable as
   coverage rather than as a number with no denominator. A count the artifact
   does not record is reported as unverifiable, never defaulted to zero.

There is no implicit floor. With a baseline, `regressed == []` already implies
`len(scanned) >= len(expected)`, so an implicit `len(scanned) >= len(expected)`
floor would be unreachable code dressed up as a second opinion. `--min-scanned`
still applies when you pass it explicitly, and with no baseline available (a
first run, or a checkout with no committed results.json) it degrades to "> 0"
and says the regression check was skipped, loudly, instead of pretending it
verified something.

Usage:

    coverage_guard.py --manifest images.txt --results results.json \
                      [--baseline previous-results.json] [--min-scanned N]

Exits 0 when coverage held, 1 on a coverage regression or a broken invariant,
2 on a usage/IO error.
Negative controls: scripts/tests/test_coverage_guard.py.
"""

from __future__ import annotations

import argparse
import json
import sys

# The coverage block render.py writes at schemaVersion 1.1. Absent in 1.0, which
# had nowhere to record any of it.
REQUIRED_COUNTS = ("manifestRowCount", "scenarioCount", "skippedCount")


def manifest_labels(path):
    r"""Scenario labels in images.txt, in file order, INCLUDING duplicates.

    Mirrors survey.sh's own parse, because the guard's arithmetic only means
    anything if it measures the row set the sweep actually walked:

    * a trailing CR is stripped, as `line="${line%%$'\r'}"` does;
    * a line is a comment only when `#` is at column 0, as
      `case "$line" in ''|'#'*)` does. An INDENTED `#` is a scenario row to the
      sweep, so it is one here too — this function used to `.strip()` first and
      silently disagreed with the sweep about it;
    * the label is field 1 of a `|` split, whitespace-collapsed the way
      `echo "$label" | xargs` collapses it: leading/trailing dropped, internal
      runs squeezed to a single space;
    * a row whose label is empty after that is dropped, as `[ -z "$label" ]`
      does;
    * duplicates are KEPT, because the sweep walks and counts them twice. This
      function used to deduplicate.

    Not mirrored, deliberately: `xargs` also applies shell quote/backslash
    processing, which nothing in images.txt uses. If a label ever needs it, the
    accounting check below is what catches the disagreement — it compares this
    row count against the count survey.sh recorded, so a drift fails the run
    instead of quietly shifting the denominator.
    """
    labels = []
    with open(path) as f:
        for raw in f:
            line = raw.rstrip("\n")
            if line.endswith("\r"):
                line = line[:-1]
            if not line or line.startswith("#"):
                continue
            label = " ".join(line.split("|")[0].split())
            if not label:
                continue
            labels.append(label)
    return labels


def load_results(path):
    """Parse a results.json."""
    with open(path) as f:
        return json.load(f)


def scenario_labels_of(doc):
    """Labels that produced a scorecard in a parsed results.json."""
    return {s.get("label", "") for s in doc.get("scenarios", [])}


def scenario_labels(results_path):
    """Labels that produced a scorecard in a results.json on disk."""
    return scenario_labels_of(load_results(results_path))


def skip_index_of(doc):
    """label -> {stage, reason} for the scenarios this run recorded as skipped.

    Empty for a schema-1.0 results.json, which had nowhere to record them; the
    guard still works, it just cannot explain the regression it found.
    """
    return {s.get("label", ""): s for s in doc.get("skipped", [])}


def check_accounting(doc, manifest):
    """Verify `scenarioCount + skippedCount == manifestRowCount == len(manifest)`.

    Returns a list of problems; empty means the invariant holds. The PR that
    introduced the coverage block advertised this invariant as checkable from
    the artifact and then checked it nowhere, which is how results.md could
    print "2 of 3 manifest rows — every row was scanned" and exit 0.

    A count the artifact does not record is a problem, not a zero. Reaching for
    `doc.get("skippedCount", 0)` here would turn "schema 1.0 could not record
    this" into "the invariant holds", which is the exact fail-quiet shape this
    guard exists to kill.
    """
    problems = []

    missing = [k for k in REQUIRED_COUNTS if not isinstance(doc.get(k), int)]
    if missing:
        problems.append(
            f"results.json does not record {', '.join(missing)} "
            f"(schemaVersion {doc.get('schemaVersion', 'unset')!r}; 1.1 records "
            "them): the coverage invariant is NOT verified, and is not assumed "
            "to hold")
        return problems

    n_manifest = doc["manifestRowCount"]
    n_scen = doc["scenarioCount"]
    n_skip = doc["skippedCount"]

    # Each counter must describe the array it sits next to, or the invariant is
    # arithmetic over numbers nobody produced.
    if n_scen != len(doc.get("scenarios", [])):
        problems.append(
            f"scenarioCount is {n_scen} but scenarios[] has "
            f"{len(doc.get('scenarios', []))} entr(ies)")
    if n_skip != len(doc.get("skipped", [])):
        problems.append(
            f"skippedCount is {n_skip} but skipped[] has "
            f"{len(doc.get('skipped', []))} entr(ies)")

    if n_scen + n_skip != n_manifest:
        gap = n_manifest - n_scen - n_skip
        if gap > 0:
            problems.append(
                f"{gap} manifest row(s) are unaccounted for: scenarioCount "
                f"({n_scen}) + skippedCount ({n_skip}) = {n_scen + n_skip}, "
                f"manifestRowCount is {n_manifest}. A row that is neither "
                "scored nor recorded as skipped is exactly the hole IRO-727 "
                "closed")
        else:
            problems.append(
                f"scenarioCount ({n_scen}) + skippedCount ({n_skip}) = "
                f"{n_scen + n_skip} exceeds manifestRowCount ({n_manifest}): "
                "the run produced output for rows the manifest never asked for")

    if n_manifest != len(manifest):
        problems.append(
            f"manifestRowCount is {n_manifest} but this guard parses "
            f"{len(manifest)} row(s) out of the manifest: the sweep and the "
            "guard disagree about what images.txt asks for, so every coverage "
            "number here is measured against the wrong denominator")

    return problems


def evaluate(manifest, scanned, baseline, min_scanned=None):
    """Decide whether coverage held.

    manifest -- labels the manifest asked for (list, file order, may repeat)
    scanned  -- labels that produced a scorecard this run (set)
    baseline -- labels that produced one in the committed results.json, or None
                when unavailable
    Returns (ok, floor, regressed, messages). `floor` is None when no absolute
    floor applies, which is the normal case once a baseline exists.
    """
    messages = []
    regressed = []

    if baseline is None:
        floor = 1 if min_scanned is None else min_scanned
        messages.append(
            "no baseline results.json: coverage regression is UNCHECKED this "
            f"run, falling back to a floor of {floor}")
    else:
        # Only rows the manifest still asks for, once each. Deleting a dead row
        # is the intended fix for a permanently failing scenario, not a
        # regression.
        seen = set()
        expected = []
        for lab in manifest:
            if lab in baseline and lab not in seen:
                seen.add(lab)
                expected.append(lab)
        regressed = [lab for lab in expected if lab not in scanned]
        # No implicit floor: `regressed == []` already implies
        # `len(scanned) >= len(expected)`, so one would never fire.
        floor = min_scanned

    ok = not regressed and (floor is None or len(scanned) >= floor)
    return ok, floor, regressed, messages


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--results", required=True,
                    help="results.json this run just wrote")
    ap.add_argument("--baseline", default="",
                    help="the last COMMITTED results.json, read from git rather "
                         "than from the working tree this run overwrites")
    ap.add_argument("--min-scanned", type=int, default=None,
                    help="fail unless at least N scenarios scanned; applies "
                         "whether or not a baseline is present (default: no "
                         "absolute floor with a baseline, 1 without one)")
    args = ap.parse_args(argv)

    try:
        manifest = manifest_labels(args.manifest)
        doc = load_results(args.results)
        scanned = scenario_labels_of(doc)
        skips = skip_index_of(doc)
        baseline = scenario_labels(args.baseline) if args.baseline else None
    except (OSError, ValueError, KeyError) as exc:
        print(f"coverage guard: cannot read inputs: {exc}", file=sys.stderr)
        return 2

    ok, floor, regressed, messages = evaluate(
        manifest, scanned, baseline, args.min_scanned)
    problems = check_accounting(doc, manifest)

    floor_note = "no floor, regression-relative" if floor is None \
        else f"floor {floor}"
    print(f"coverage: {len(scanned)} scanned / {len(manifest)} manifest rows "
          f"/ {len(skips)} recorded skips ({floor_note})", file=sys.stderr)
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
    elif floor is not None and len(scanned) < floor:
        print(f"coverage guard: FAIL — only {len(scanned)} scenarios scanned, "
              f"floor is {floor}.", file=sys.stderr)

    if problems:
        print("coverage guard: FAIL — the artifact does not account for every "
              "manifest row (scenarioCount + skippedCount == manifestRowCount):",
              file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)

    return 0 if (ok and not problems) else 1


if __name__ == "__main__":
    sys.exit(main())
