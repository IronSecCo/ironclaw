# IronClaw Adoption Metrics

A tracked snapshot of IronClaw's adoption signals: stars, repo traffic (views + clones),
release downloads, referrers, and community activity. This is the single place we record
**where we are vs. where we want to be**, refreshed weekly.

- **Owner:** Growth / DevRel (WS-E)
- **Cadence:** weekly (Mondays), via the `community-metrics-weekly` routine
- **Refresh command:** `scripts/community/adoption-snapshot.sh` (prints a ready-to-paste snapshot block)
- **Data source:** GitHub REST API — `repos/IronSecCo/ironclaw` + `/traffic/*` + `/releases`
  (traffic endpoints require maintainer push access)

## How to read these numbers (honesty notes)

Credibility over hype — for a security audience, accurate measurement is the brand. Two
caveats apply to every snapshot:

1. **Clone counts are CI-inflated.** `release.yml` cuts a release on every push to `main`,
   and CI runners clone the repo each run. The raw `clones.count` is therefore dominated by
   our own automation. **Unique cloners** is better but still noisy. Treat **stars** and
   **unique visitors** as the honest external-adoption signal, especially pre-launch.
2. **Traffic is a 14-day rolling window.** GitHub only exposes the trailing 14 days for
   views/clones/referrers. We snapshot weekly so we keep a longer record than the API does.

## Baseline & targets

Repo went public **2026-06-16**. The launch (IRO-40) is **human-gated and not yet fired** —
so the numbers below are the **pre-launch baseline**, not a launched-product readout.

| Signal | Baseline (2026-06-30) | Launch-week target | 30-day post-launch target |
| --- | --- | --- | --- |
| Stars | 11 | 150+ | 500+ |
| Unique visitors (14d) | 44 | 1,000+ | 2,500+ |
| Forks | 2 | 20+ | 60+ |
| Release downloads (latest tag) | 24 | 200+ | 750+ |
| Discussions (non-seed threads) | 0 | 10+ | 30+ |
| External contributors (PRs merged) | 0 | 1+ | 5+ |

Targets are directional, set against comparable security-OSS launches at similar stage;
revise after the first real launch-week data lands.

## Weekly snapshots

> Newest first. Append a new block each Monday by running the refresh command and pasting
> its output above the previous block.

### Snapshot — 2026-06-30 (pre-launch baseline)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 11 | |
| Forks | 2 | |
| Watchers | 0 | |
| Open issues | 13 | incl. tracked work + GFIs |
| Views (14d) | 471 | 44 unique visitors |
| Clones (14d) | 6905 | 632 unique — **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 1451 | across 91 releases |
| Latest release | v0.1.112 | 24 downloads |
| Top referrers | — | github.com (18u) · cla-assistant.io (2u) · reddit.com (3u) · goodfirstissues.com (2u) |

> Clone counts are dominated by CI runners (a release is cut on every push to main).
> Treat **unique visitors** and **stars** as the honest adoption signal pre-launch.

**Read:** Genuinely early/quiet, exactly as expected for a pre-launch repo. The signal worth
noting is that `reddit.com` and `goodfirstissues.com` already appear as referrers — the
good-first-issue seeding (IRO-209) and early directory submissions are pulling a trickle of
real humans before any announcement. Discussions are seeded but have no organic threads yet.

## Related

- Launch engagement playbook: [launch-engagement-playbook.md](launch-engagement-playbook.md)
- Launch gate / readiness: tracked in IRO-40
- Directory & awesome-list discoverability: IRO-177, IRO-98
