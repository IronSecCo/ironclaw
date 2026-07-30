#!/usr/bin/env python3
"""Measure what GitHub's scheduler actually delivers for this repo's `schedule:` workflows.

Motivation (IRO-679): two safety nets for the rolling Homebrew bump are `schedule:` crons,
and both carry a documented latency promise. A cron promise is only worth what the
scheduler delivers, and GitHub documents that `schedule` events are delayed or dropped
under load. This script measures the delivery so the promises can be checked against it
instead of against the cron expression.

The metric is EXCESS OVER NOMINAL PERIOD: for consecutive runs of one workflow, the
observed gap minus the period the cron asks for. That is the quantity a "detection latency
is <= N minutes" claim actually rests on.

Also reported: LIVE SILENCE, the still-open interval between the last scheduled run and
now. Gaps need two runs, so on a cron that is being dropped hard the gap sample is tiny
and cannot carry a conclusion by itself -- whereas "it has been N minutes with nothing
pending" is a single direct observation that does not depend on the sample size at all.
That is the load-bearing evidence on IRO-679, so it is measured here rather than by hand.
Silence is only reported for an ACTIVE workflow with no run in flight: a disabled or
auto-disabled workflow is silent for a reason that has nothing to do with the scheduler,
and reporting that as latency would be a false positive.

Deliberately NOT measured: delay from the declared clock time. For a high-frequency cron
every slot is close to every run, so nearest-slot matching cannot produce a delay larger
than the period -- it reports a small number no matter how badly the cron is being dropped.
That metric flatters exactly the case we care about, so it is not used here. (Phase drift
is reported separately and read-only, because it is real -- see --phase -- but it does not
affect cadence and no promise in this repo depends on it.)

Usage:
    scripts/measure-cron-latency.py                 # all schedule: workflows in this repo
    scripts/measure-cron-latency.py --phase         # also report declared-vs-actual clock time
    scripts/measure-cron-latency.py --repo O/N      # another repo

Requires `gh` authenticated with read access to the repo's Actions history. Read-only:
issues no writes and takes no action on any workflow.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import pathlib
import re
import statistics
import subprocess
import sys

# Periods we can score. A cron only has a well-defined nominal period when it fires on a
# fixed stride; anything else (e.g. "0 6 * * 1,4") is listed but not scored, because
# "excess over nominal" is meaningless without a single nominal.
_MINUTE_STRIDE = re.compile(r"^\*/(\d+)$")


def _period_minutes(cron: str) -> int | None:
    """Nominal period of a 5-field cron in minutes, or None if it has no single stride."""
    try:
        minute, hour, dom, month, dow = cron.split()
    except ValueError:
        return None
    if month != "*" or dom != "*":
        return None
    stride = _MINUTE_STRIDE.match(minute)
    if stride and hour == "*" and dow == "*":
        return int(stride.group(1))
    # A comma list of fixed minutes at every hour, e.g. "13,43 * * * *".
    if hour == "*" and dow == "*" and all(p.isdigit() for p in minute.split(",")):
        parts = sorted(int(p) for p in minute.split(","))
        if len(parts) == 1:
            return 60
        gaps = {b - a for a, b in zip(parts, parts[1:])} | {parts[0] + 60 - parts[-1]}
        return gaps.pop() if len(gaps) == 1 else None
    if not minute.isdigit():
        return None
    if hour == "*" and dow == "*":
        return 60
    if hour.isdigit() and dow == "*":
        return 1440
    if hour.isdigit() and dow.isdigit():
        return 10080
    return None


def _crons(path: pathlib.Path) -> list[str]:
    """Cron expressions under a `schedule:` block. Text-scanned on purpose: PyYAML is not a
    dependency of this repo and a wrong answer here is visible, not silent."""
    out: list[str] = []
    in_schedule = False
    saw_schedule = False
    for line in path.read_text().splitlines():
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if re.match(r"^schedule:\s*$", stripped):
            in_schedule = saw_schedule = True
            continue
        if in_schedule:
            # Trailing `# ...` comments after the cron are common in this repo, so strip
            # them before matching rather than requiring end-of-line right after the quote.
            body = re.sub(r"\s+#.*$", "", stripped)
            m = re.match(r"^-\s*cron:\s*[\"']?([^\"']+?)[\"']?\s*$", body)
            if m:
                out.append(m.group(1).strip())
                continue
            if body and not body.startswith("-"):
                in_schedule = False
    if saw_schedule and not out:
        # A parser that silently drops a scheduled workflow reports a smaller, healthier
        # looking sample than reality. Refuse to do that quietly.
        raise SystemExit(f"{path.name}: has a `schedule:` block but no cron parsed out of it")
    return out


def _runs(repo: str, workflow: str) -> tuple[list[dt.datetime], bool]:
    """(created_at of every scheduled run oldest-first, whether one is still in flight).

    created_at is when GitHub created the run, i.e. when the schedule actually fired --
    not when a runner picked it up. The in-flight flag matters for the silence column:
    a queued run means the scheduler has already fired and we are merely waiting on a
    runner, which is not the failure this script is looking for."""
    proc = subprocess.run(
        ["gh", "api", "--paginate",
         f"repos/{repo}/actions/workflows/{workflow}/runs?event=schedule&per_page=100",
         "--jq", r'.workflow_runs[] | "\(.created_at) \(.status)"'],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        return [], False
    stamps: list[dt.datetime] = []
    in_flight = False
    for line in proc.stdout.splitlines():
        if not line.strip():
            continue
        created, _, status = line.partition(" ")
        stamps.append(dt.datetime.fromisoformat(created.replace("Z", "+00:00")))
        if status.strip() != "completed":
            in_flight = True
    return sorted(stamps), in_flight


def _state(repo: str, workflow: str) -> str:
    """The workflow's `state` (active / disabled_manually / disabled_inactivity / ...).

    Load-bearing negative control. GitHub auto-disables scheduled workflows in dormant
    repos, and a disabled cron is perfectly silent -- scoring that as scheduler latency
    would manufacture the exact finding this script exists to test."""
    proc = subprocess.run(
        ["gh", "api", f"repos/{repo}/actions/workflows/{workflow}", "--jq", ".state"],
        capture_output=True, text=True,
    )
    return proc.stdout.strip() if proc.returncode == 0 else "unknown"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default="IronSecCo/ironclaw")
    ap.add_argument("--phase", action="store_true",
                    help="also report declared vs actual clock time (drift, not cadence)")
    args = ap.parse_args()

    wf_dir = pathlib.Path(__file__).resolve().parent.parent / ".github" / "workflows"
    now = dt.datetime.now(dt.timezone.utc)
    pooled: list[tuple[str, float]] = []
    rows = []

    for path in sorted(wf_dir.glob("*.yml")):
        crons = _crons(path)
        if len(crons) != 1:
            # Zero: not scheduled. More than one: no single nominal period to score.
            continue
        cron = crons[0]
        period = _period_minutes(cron)
        runs, in_flight = _runs(args.repo, path.name)
        gaps = [(b - a).total_seconds() / 60 for a, b in zip(runs, runs[1:])]
        rows.append((path.name, cron, period, runs, gaps,
                     _state(args.repo, path.name), in_flight))
        if period and gaps:
            # Pool only cadences we have enough of to say anything about, and keep the
            # sub-hourly arm out of the pool -- it is the hypothesis under test, not evidence.
            if period >= 1440:
                pooled += [(path.name, g - period) for g in gaps]

    print(f"repo: {args.repo}    measured: {now:%Y-%m-%dT%H:%MZ}\n")
    print(f"{'workflow':<34}{'cron':<16}{'runs':>5}{'gaps':>5}   excess over nominal (min)")
    print("-" * 96)
    for name, cron, period, runs, gaps, state, in_flight in rows:
        if not period:
            print(f"{name:<34}{cron:<16}{len(runs):>5}{len(gaps):>5}   (no single nominal period)")
            continue
        if not gaps:
            print(f"{name:<34}{cron:<16}{len(runs):>5}{len(gaps):>5}   (no interval yet)")
            continue
        ex = sorted(g - period for g in gaps)
        flag = "  <-- exceeds its own period" if max(ex) > period else ""
        print(f"{name:<34}{cron:<16}{len(runs):>5}{len(gaps):>5}   "
              f"min {min(ex):+.0f}  p50 {statistics.median(ex):+.0f}  max {max(ex):+.0f}{flag}")

    print(f"\n{'workflow':<34}{'cron':<16}   live silence since last run")
    print("-" * 96)
    for name, cron, period, runs, _gaps, state, in_flight in rows:
        if not runs:
            print(f"{name:<34}{cron:<16}   (never fired on a schedule)")
            continue
        silence = (now - runs[-1]).total_seconds() / 60
        if state != "active":
            # Never score a disabled workflow's silence. See _state().
            print(f"{name:<34}{cron:<16}   {silence:.0f} min, NOT SCORED (state: {state})")
            continue
        if in_flight:
            print(f"{name:<34}{cron:<16}   {silence:.0f} min, NOT SCORED (a run is in flight)")
            continue
        if not period:
            print(f"{name:<34}{cron:<16}   {silence:.0f} min (no single nominal period)")
            continue
        flag = "  <-- over 2x its period, nothing pending" if silence > 2 * period else ""
        print(f"{name:<34}{cron:<16}   {silence:.0f} min vs {period} min asked "
              f"({silence / period:.1f}x){flag}")
    print("\n  A flagged row is a single direct observation and does not need a sample of")
    print("  gaps to stand up: the workflow is active, nothing is queued, and the schedule")
    print("  has still not fired. Quote this, not the gap count, when the gap count is small.")

    if pooled:
        vals = sorted(v for _, v in pooled)
        pct = lambda p: vals[min(len(vals) - 1, int(p * len(vals)))]
        print(f"\nPOOLED (daily and weekly crons only): {len(vals)} intervals")
        print(f"  excess over nominal:  p50 {pct(.5):+.0f}   p90 {pct(.9):+.0f}   "
              f"p95 {pct(.95):+.0f}   max {max(vals):+.0f} min")
        over = sum(1 for v in vals if v > 60)
        print(f"  intervals overshooting nominal by >60 min: {over}/{len(vals)}")
        print("\n  Read this as: the cadence a cron asks for is delivered to within roughly")
        print("  this much. A 'detection latency <= one period' claim must add the max above.")

    if args.phase:
        print(f"\n{'workflow':<34}{'declared':>10}{'observed fire time (UTC)':>34}")
        print("-" * 80)
        for name, cron, period, runs, _gaps, _state_, _in_flight in rows:
            if not runs or not period or period < 1440:
                continue
            f = cron.split()
            declared = f"{int(f[1]):02d}:{int(f[0]):02d}" if f[1].isdigit() else "--:--"
            tod = [r.hour * 60 + r.minute for r in runs]
            fmt = lambda m: f"{m // 60:02d}:{m % 60:02d}"
            med = int(statistics.median(tod))
            print(f"{name:<34}{declared:>10}"
                  f"{fmt(min(tod)) + '-' + fmt(max(tod)) + '  median ' + fmt(med):>34}")
        print("\n  Phase drift does not affect cadence. It does mean the declared clock time")
        print("  in a cron expression does not predict when the workflow runs, so do not")
        print("  write a doc claim of the form 'runs at HH:MM'.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
