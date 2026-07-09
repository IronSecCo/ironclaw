#!/usr/bin/env python3
"""render.py — aggregate per-scenario `ironctl scan --json` blobs into the
combined results.json + a human-readable results.md table.

Pure stdlib (json only). Reads a JSON array of records on stdin, each:

    {"label": ..., "image": ..., "runFlags": ..., "report": <scan-json>}

Writes results.json to argv[1] and results.md to argv[2]. Deterministic: rows
are sorted by (score asc, label asc) so a re-run over the same manifest yields a
byte-identical table (minus the generatedAt/version stamps, which are recorded
once at the dataset level).
"""
import json
import sys


def failed_dims(report):
    """Titles of dimensions the scanner graded FAIL or UNKNOWN (fail-closed),
    worst-weighted first — the 'top failed dimensions' column."""
    dims = [d for d in report.get("dimensions", [])
            if d.get("verdict") in ("FAIL", "UNKNOWN")]
    dims.sort(key=lambda d: -d.get("max", 0))
    return [d.get("title", d.get("key", "?")) for d in dims]


def main():
    out_json, out_md = sys.argv[1], sys.argv[2]
    records = json.load(sys.stdin)

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
    dataset = {
        "report": "ironclaw-isolation-survey",
        "schemaVersion": "1.0",
        "generatedAt": stamp.get("generatedAt", ""),
        "ironctlVersion": stamp.get("version", ""),
        "scenarioCount": len(rows),
        "scenarios": rows,
    }
    with open(out_json, "w") as f:
        json.dump(dataset, f, indent=2, sort_keys=True)
        f.write("\n")

    # Markdown table.
    lines = []
    lines.append("# State of Container Isolation — survey results")
    lines.append("")
    lines.append(f"Scanned **{len(rows)} scenarios** with "
                 f"`ironctl scan` {dataset['ironctlVersion']} "
                 f"on {dataset['generatedAt']}.")
    lines.append("")
    lines.append("Each row is one popular public image run with a specific "
                 "configuration, graded 0-100 across seven containment "
                 "dimensions (non-root user, dropped capabilities, seccomp, "
                 "network isolation, read-only rootfs, no docker.sock, no host "
                 "namespaces). Higher is safer. See "
                 "[README.md](./README.md) for the exact method and "
                 "[images.txt](./images.txt) for the pinned manifest.")
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
    dist = {}
    for r in rows:
        dist[r["grade"]] = dist.get(r["grade"], 0) + 1
    summary = ", ".join(f"{dist[g]}×{g}" for g in sorted(dist))
    lines.append(f"**Grade distribution:** {summary}.")
    lines.append("")
    lines.append("Regenerate this file from a clean checkout with "
                 "`examples/isolation-survey/survey.sh` (Docker required).")
    lines.append("")
    with open(out_md, "w") as f:
        f.write("\n".join(lines))


if __name__ == "__main__":
    main()
