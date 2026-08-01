#!/usr/bin/env python3
"""render.py — aggregate per-scenario `ironctl scan --json` blobs into the
combined results.json + a human-readable results.md table.

Pure stdlib (json only). Reads a JSON array of records on stdin, each:

    {"label": ..., "image": ..., "runFlags": ..., "report": <scan-json>}

Writes results.json to argv[1] and results.md to argv[2]. Deterministic: rows
are sorted by (score asc, label asc) so a re-run over the same manifest yields a
byte-identical table (minus the generatedAt/version stamps, which are recorded
once at the dataset level).

Coverage is part of the output, not a log line (IRO-727). `--skips` takes the
JSON array survey.sh accumulates for every scenario it dropped, each
`{"label", "image", "stage", "reason"}`; those land in `.skipped[]` of
results.json and in a "Not scanned" section of results.md, so a reader can tell
measured-and-passed from never-measured without opening an Actions log that
expires. `--manifest-rows` records how many rows the manifest actually had, so
`scenarioCount + skippedCount == manifestRowCount` is checkable from the
artifact alone — and coverage_guard.py, which survey.sh runs immediately after
this, checks it rather than leaving it to the reader.
"""
import argparse
import json
import sys

SKIP_STAGES = ("pull", "run", "scan")


def failed_dims(report):
    """Titles of dimensions the scanner graded FAIL or UNKNOWN (fail-closed),
    worst-weighted first — the 'top failed dimensions' column."""
    dims = [d for d in report.get("dimensions", [])
            if d.get("verdict") in ("FAIL", "UNKNOWN")]
    dims.sort(key=lambda d: -d.get("max", 0))
    return [d.get("title", d.get("key", "?")) for d in dims]


def load_skips(path):
    """Normalise survey.sh's skip array. Unknown stages are kept (and sorted
    last) rather than dropped: a skip we cannot classify is still a scenario
    that was never measured, and silently discarding it is the bug this whole
    change exists to fix."""
    if not path:
        return []
    with open(path) as f:
        raw = json.load(f)
    skips = []
    for s in raw:
        skips.append({
            "label": s.get("label", ""),
            "image": s.get("image", ""),
            "stage": s.get("stage", "unknown"),
            "reason": (s.get("reason", "") or "").strip(),
        })
    skips.sort(key=lambda s: (s["label"], s["stage"]))
    return skips


def skip_counts(skips):
    """stage -> count, listing the three known stages in pipeline order first."""
    counts = {}
    for s in skips:
        counts[s["stage"]] = counts.get(s["stage"], 0) + 1
    ordered = {st: counts[st] for st in SKIP_STAGES if st in counts}
    for st in sorted(counts):
        ordered.setdefault(st, counts[st])
    return ordered


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("out_json")
    ap.add_argument("out_md")
    ap.add_argument("--skips", default="",
                    help="JSON array of {label,image,stage,reason} scenarios "
                         "the survey dropped")
    ap.add_argument("--manifest-rows", type=int, default=None,
                    help="number of scenario rows in images.txt")
    args = ap.parse_args()

    out_json, out_md = args.out_json, args.out_md
    records = json.load(sys.stdin)
    skips = load_skips(args.skips)

    rows = []
    for rec in records:
        rep = rec["report"]
        rows.append({
            "label": rec["label"],
            "image": rec["image"],
            "resolvedDigest": rec.get("resolvedDigest", ""),
            "runFlags": rec.get("runFlags", "").strip(),
            "score": rep.get("score", 0),
            "grade": rep.get("grade", "?"),
            "failedDimensions": failed_dims(rep),
            "report": rep,
        })
    rows.sort(key=lambda r: (r["score"], r["label"]))

    # A dataset-level stamp: take the tool version + generatedAt from the first
    # report (they are identical across a single run).
    stamp = records[0]["report"] if records else {}
    manifest_rows = args.manifest_rows
    if manifest_rows is None:
        manifest_rows = len(rows) + len(skips)
    dataset = {
        "report": "ironclaw-isolation-survey",
        # 1.1 adds the coverage block: manifestRowCount, skippedCount, skipped[].
        "schemaVersion": "1.1",
        "generatedAt": stamp.get("generatedAt", ""),
        "ironctlVersion": stamp.get("version", ""),
        "manifestRowCount": manifest_rows,
        "scenarioCount": len(rows),
        "skippedCount": len(skips),
        "scenarios": rows,
        "skipped": skips,
    }
    with open(out_json, "w") as f:
        json.dump(dataset, f, indent=2, sort_keys=True)
        f.write("\n")

    # Markdown table.
    lines = []
    lines.append("# State of Container Isolation — survey results")
    lines.append("")
    if rows:
        lines.append(f"Scanned **{len(rows)} scenarios** with "
                     f"`ironctl scan` {dataset['ironctlVersion']} "
                     f"on {dataset['generatedAt']}.")
    else:
        lines.append("**No scenario produced a scorecard in this run.** Every "
                     "manifest row is accounted for below.")
    lines.append("")

    # "Every row was scanned" is derived from the arithmetic, never from an
    # empty skip list. Those are not the same claim: a run can drop a row
    # without recording a skip, and reading the sentence off `if skips:` printed
    # "Coverage: 2 of 3 manifest rows — every row was scanned" at exit 0
    # (IRO-727). coverage_guard.py fails a run where they diverge; this file
    # still has to describe what it actually has.
    unaccounted = manifest_rows - len(rows) - len(skips)
    if len(rows) == manifest_rows and not skips:
        lines.append(f"**Coverage: {len(rows)} of {manifest_rows} manifest "
                     "rows — every row was scanned.**")
    else:
        note = f"**Coverage: {len(rows)} of {manifest_rows} manifest rows.**"
        if skips:
            breakdown = ", ".join(f"{stage} {n}"
                                  for stage, n in skip_counts(skips).items())
            note += (f" {len(skips)} scenario(s) were dropped before they could "
                     f"be graded ({breakdown}) and are listed under "
                     "[Not scanned](#not-scanned) below — they are absent from "
                     "the table, not scored zero.")
        if unaccounted:
            note += (f" {abs(unaccounted)} row(s) are **unaccounted for**: "
                     f"{len(rows)} scored + {len(skips)} recorded as skipped "
                     f"does not equal {manifest_rows}. That gap is a bug in the "
                     "harness, not a property of the images.")
        lines.append(note)
    lines.append("")
    lines.append("Each row is one popular public image run with a specific "
                 "configuration, graded 0-100 across seven containment "
                 "dimensions (non-root user, dropped capabilities, seccomp, "
                 "network isolation, read-only rootfs, no docker.sock, no host "
                 "namespaces). Higher is safer. See "
                 "[README.md](./README.md) for the exact method and "
                 "[images.txt](./images.txt) for the scenario manifest.")
    lines.append("")
    lines.append("| Scenario | Image | Score | Grade | Top failed dimensions |")
    lines.append("|----------|-------|------:|:-----:|-----------------------|")
    for r in rows:
        failed = ", ".join(r["failedDimensions"][:3]) or "none"
        img = r["image"].split("@")[0]  # drop the digest for readability
        lines.append(f"| `{r['label']}` | `{img}` | {r['score']}/100 "
                     f"| **{r['grade']}** | {failed} |")
    lines.append("")

    # A compact grade-distribution summary.
    if rows:
        dist = {}
        for r in rows:
            dist[r["grade"]] = dist.get(r["grade"], 0) + 1
        summary = ", ".join(f"{dist[g]}×{g}" for g in sorted(dist))
        lines.append(f"**Grade distribution:** {summary}.")
        lines.append("")

    # Every dropped scenario, by name. A count alone still leaves the reader
    # guessing which images the dataset does not cover (IRO-727).
    if skips:
        lines.append("## Not scanned")
        lines.append("")
        lines.append(f"{len(skips)} of the {manifest_rows} rows in "
                     "[images.txt](./images.txt) produced no scorecard this "
                     "run. `pull` = the image could not be fetched from the "
                     "mirror or its original registry, `run` = `docker run "
                     "--entrypoint sleep` did not start it, `scan` = the "
                     "container started but `ironctl scan` failed. These rows "
                     "are **not measured**; they are not a low score.")
        lines.append("")
        lines.append("| Scenario | Image | Failed at | Reason |")
        lines.append("|----------|-------|-----------|--------|")
        for s in skips:
            reason = s["reason"].replace("|", "\\|") or "(no detail recorded)"
            lines.append(f"| `{s['label']}` | `{s['image']}` | {s['stage']} "
                         f"| {reason} |")
        lines.append("")
    lines.append("Regenerate this file from a clean checkout with "
                 "`examples/isolation-survey/survey.sh` (Docker required).")
    lines.append("")
    with open(out_md, "w") as f:
        f.write("\n".join(lines))


if __name__ == "__main__":
    main()
