#!/usr/bin/env bash
# adoption-snapshot.sh — print a Markdown adoption-metrics snapshot for IronClaw.
#
# Pulls stars/forks, 14-day traffic (views + clones), top referrers, and aggregate
# release download counts via the GitHub API, then emits a Markdown block you can paste
# into community/adoption-metrics.md as the latest weekly snapshot.
#
# Requires: gh (authenticated with push access — traffic endpoints need it), python3.
# Usage:    scripts/community/adoption-snapshot.sh
set -euo pipefail

REPO="${IRONCLAW_REPO:-IronSecCo/ironclaw}"
TODAY="$(date -u +%Y-%m-%d)"

command -v gh >/dev/null || { echo "error: gh not found" >&2; exit 1; }
command -v python3 >/dev/null || { echo "error: python3 not found" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

gh api "repos/${REPO}" --jq '{stars:.stargazers_count, forks:.forks_count, watchers:.subscribers_count, open_issues:.open_issues_count}' > "$work/core.json"
gh api "repos/${REPO}/traffic/views"   > "$work/views.json"
gh api "repos/${REPO}/traffic/clones"  > "$work/clones.json"
gh api "repos/${REPO}/traffic/popular/referrers" > "$work/referrers.json"
# Slim releases server-side to avoid huge payloads: keep only what we aggregate.
gh api "repos/${REPO}/releases" --paginate \
  --jq '.[] | {tag_name, published_at, downloads: ([.assets[].download_count] | add // 0)}' \
  | python3 -c 'import sys,json; print(json.dumps([json.loads(l) for l in sys.stdin if l.strip()]))' \
  > "$work/releases.json"

WORK="$work" TODAY="$TODAY" python3 <<'PY'
import json, os
w = os.environ["WORK"]
load = lambda n: json.load(open(os.path.join(w, n)))

core, views, clones, referrers, releases = (
    load("core.json"), load("views.json"), load("clones.json"),
    load("referrers.json"), load("releases.json"),
)

dl_total = sum(r.get("downloads", 0) for r in releases)
latest = max(releases, key=lambda r: r.get("published_at", "")) if releases else None
top_ref = " · ".join(f"{r['referrer']} ({r['uniques']}u)" for r in referrers[:4]) or "—"

print(f"### Snapshot — {os.environ['TODAY']}\n")
print("| Metric | Value | Notes |")
print("| --- | --- | --- |")
print(f"| Stars | {core['stars']} | |")
print(f"| Forks | {core['forks']} | |")
print(f"| Watchers | {core['watchers']} | |")
print(f"| Open issues | {core['open_issues']} | incl. tracked work + GFIs |")
print(f"| Views (14d) | {views['count']} | {views['uniques']} unique visitors |")
print(f"| Clones (14d) | {clones['count']} | {clones['uniques']} unique — **CI-inflated** (release-per-push) |")
print(f"| Release downloads (all-time) | {dl_total} | across {len(releases)} releases |")
print(f"| Latest release | {latest['tag_name'] if latest else '—'} | {latest['downloads'] if latest else 0} downloads |")
print(f"| Top referrers | — | {top_ref} |")
print()
print("> Clone counts are dominated by CI runners (a release is cut on every push to main).")
print("> Treat **unique visitors** and **stars** as the honest adoption signal pre-launch.")
PY
