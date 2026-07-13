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

### Snapshot — 2026-07-13 (steady-state baseline, IRO-483)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 14 | +1 vs 2026-07-06 (13) — first move off the four-snapshot flat plateau |
| Forks | 9 | **+7 vs 2026-07-06 (2)** — largest single-week jump on record |
| Watchers / subscribers | 0 | |
| Open issues | 26 | +8 vs 2026-07-06 (18); incl. tracked work + open PRs + GFIs |
| Views (14d) | 335 | 50 unique visitors (-4 vs 2026-07-06's 54) |
| Clones (14d) | 25388 | 804 unique, **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 4099 | +1069 vs 2026-07-06, across 203 releases (all CI) |
| Latest release | v0.1.298 | 23 downloads |
| Top referrers | — | github.com (13u) · **linkedin.com (10u) + com.linkedin.android (5u)** · goodfirstissues.com (2u) |

**Delta readout vs 2026-07-06:**

- **Forks jumped 2 → 9 (+7), the largest single-week fork gain on record.** Forks lead stars
  as the contributor-intent signal: this is the first snapshot where the good-first-issue and
  scan/scores on-ramp work (IRO-422 GFIs, external PR #441) shows up as durable repo activity
  rather than one-off referrer blips. Fork growth outpacing star growth means people are cloning
  to *build on* IronClaw, not just bookmarking it.
- **Stars ticked 13 → 14, breaking a four-snapshot flat line** (2026-07-02 → 07-06 all at 13).
  Directionally positive but still small; the reach thesis (IRO-355) holds — distribution, not
  features, remains the binding constraint. One star is noise-adjacent; the fork move is the
  more credible external-adoption signal this week.
- **`goodfirstissues.com` re-entered the trailing-14-day referrer window** (2u), consistent with
  the fork jump — the GFI aggregator surfaces (IRO-464) are feeding contributor-shaped traffic.
  LinkedIn remains the strongest live external channel (15 combined unique referrers: 10 web + 5
  Android), roughly flat vs last week.
- **Unique visitors eased 54 → 50 (-4); views fell 481 → 335.** Within normal week-to-week
  noise for a pre-launch repo — not a reach regression, and offset by the fork/contributor signal.
- **Bottleneck unchanged and board-gated:** high-intent top-of-funnel surfaces (Show HN, Reddit
  launch threads, remaining directory submissions) still need human/board posting (IRO-290,
  IRO-306, IRO-355). Owned growth work continues to convert on the contributor funnel (forks, GFIs)
  but the star step-change stays gated on those human-account posts firing.

### Snapshot — 2026-07-06 (steady-state baseline, IRO-392)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 13 | **flat** vs 2026-07-05 (13) and the IRO-286 baseline — reach still not converting |
| Forks | 2 | flat |
| Watchers / subscribers | 0 | |
| Open issues | 18 | incl. tracked work + open PRs + GFIs |
| Views (14d) | 481 | 54 unique visitors (-1 vs 2026-07-05, effectively flat) |
| Clones (14d) | 11906 | 870 unique, **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 3030 | +277 vs 2026-07-05, across 158 releases (all CI) |
| Latest release | v0.1.216 | 23 downloads |
| Top referrers | — | github.com (19u) · **linkedin.com (9u) + com.linkedin.android (5u)** · cla-assistant.io (2u) |

**Delta readout vs 13-star baseline (IRO-286):**

- **Stars flat at 13 for a fourth straight snapshot** (2026-07-02 → 07-03 → 07-05 → 07-06). The
  reach thesis (IRO-355) holds: distribution, not features, is the binding constraint. Everything
  merged this week (MCP-server onboarding, AutoGen/SK integrations, containment benchmark blog) is
  live but under-distributed.
- **LinkedIn remains the only strong live external channel** — 14 combined unique referrers (9 web
  + 5 Android), unchanged from 2026-07-05. It is the single non-GitHub source still driving humans.
- **`reddit.com`, `goodfirstissues.com`, and Google dropped out of the trailing-14-day window.**
  This is not a loss of interest so much as decay: no fresh inbound push on those surfaces in the
  last two weeks, so their older hits aged out. The 14-day window punishes any channel we are not
  actively feeding.
- **Unique visitors flat (55 → 54).** Views fell (546 → 481) but uniques held, so the drop is
  repeat/CI traffic, not lost reach.
- **Bottleneck unchanged and board-gated:** the high-intent top-of-funnel surfaces (Show HN,
  Reddit launch threads, remaining directory submissions) need human/board posting (IRO-290,
  IRO-306, IRO-355). Owned growth work has saturated what it can move without those posts firing;
  the flat star line is the direct readout of that gate.

### Snapshot — 2026-07-05 (post-launch reach round 2)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 13 | **flat** vs 2026-07-03 (13) and the IRO-286 baseline — reach is not yet converting |
| Forks | 2 | flat |
| Watchers / subscribers | 0 | |
| Open issues | 17 | incl. tracked work + open PRs + GFIs |
| Views (14d) | 546 | 55 unique visitors (+4 uniques vs 2026-07-03) |
| Clones (14d) | 10890 | 881 unique, **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 2753 | +619 vs 2026-07-03, across 146 releases (all CI) |
| Latest release | v0.1.200 | 24 downloads |
| Top referrers | — | github.com (18u) · **linkedin.com (9u) + com.linkedin.android (5u)** · cla-assistant.io (3u) · reddit.com (3u) · **goodfirstissues.com (2u)** · Google (2u) |

**Delta readout vs 13-star baseline (IRO-286):**

- **Stars flat at 13.** No net-new stars since 2026-07-03. Confirms the IRO-355 thesis: the
  #1 lever is *reach*, not features. The merged content (integration examples + SEO cluster +
  2 follow-up blog posts) is live but under-distributed.
- **LinkedIn is the strongest live external channel** — 14 combined unique referrers (9 web +
  5 Android), up from 3u on 2026-07-03. The launch posts that are already live are driving the
  most non-GitHub traffic.
- **`goodfirstissues.com` now appears (2u)** — the GFI seeding (IRO-263/286) is producing an
  organic contributor on-ramp with zero paid spend.
- **reddit.com steady at 3u**, Google organic surfacing (2u) as the SEO cluster indexes.
- **Bottleneck is unchanged and board-gated:** the high-intent surfaces (Show HN, Reddit
  threads, remaining directory submissions) require human/board posting (IRO-290, IRO-306).
  Traffic exists; the star conversion needs those top-of-funnel posts to fire.

### Snapshot — 2026-07-03 (blog live, external threads not yet posted)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 13 | flat vs 2026-07-02 (13) and the IRO-286 baseline |
| Forks | 2 | flat |
| Watchers / subscribers | 0 | |
| Open issues | 19 | includes tracked work + open PRs + GFIs |
| Views (14d) | 580 | 51 unique visitors (flat) |
| Clones (14d) | 8637 | 801 unique, **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 2134 | +92 vs 2026-07-02 (across 120 releases, all CI) |
| Latest release | v0.1.158 | 23 downloads |
| Top referrers | — | github.com (19u) · cla-assistant.io (3u) · linkedin.com (3u) · reddit.com (3u) |

**Read:** Flat. Stars 13 -> 13, unique visitors 51 -> 51, no movement in the honest
external signals. This is the expected result: the launch blog is published
(`breaking-our-own-sandbox`, IRO-271), but the reach levers that actually move these
numbers, the Show HN / Reddit launch threads and the remaining directory submissions,
are **owner-manual** and have not been posted yet (external, no in-environment
credentials; identity guardrails apply). Two follow-up posts shipped this round
(gVisor deep-dive, bring-your-own-model), which grows the surface a visitor can land
on but does not by itself drive traffic. The bottleneck for round 2 is distribution
execution (owner posts the staged threads), not more content. Referrer mix narrowed
vs 2026-07-02: `goodfirstissues.com`, `google`, and the docs site dropped out of the
trailing 14-day window, consistent with no fresh inbound push.

### Snapshot — 2026-07-02 (pre-launch)

| Metric | Value | Notes |
| --- | --- | --- |
| Stars | 13 | +2 vs. 2026-06-30 baseline |
| Forks | 2 | flat |
| Watchers / subscribers | 0 | |
| Open issues | 17 | 16 are scoped good-first-issues (see below) |
| Views (14d) | 580 | 51 unique visitors (was 44) |
| Clones (14d) | 8637 | 801 unique — **CI-inflated** (release-per-push) |
| Release downloads (all-time) | 2042 | across 116 releases |
| Latest release | v0.1.153 | 23 downloads |
| Top referrers | — | github.com (19u) · cla-assistant.io (3u) · linkedin.com (3u) · reddit.com (3u) · goodfirstissues.com (2u) · Google (2u) · ironsecco.github.io (1u) |

> Clone counts are dominated by CI runners (a release is cut on every push to main).
> Treat **unique visitors** and **stars** as the honest adoption signal pre-launch.

**Read:** Still genuinely early/quiet, as expected pre-launch, but the trend is up and
slightly wider: stars 11 → 13, unique visitors 44 → 51. The referrer mix is the honest
signal — `goodfirstissues.com` persists (GFI seeding working), and two new organic sources
appear: `linkedin.com` / `com.linkedin.android` and the docs site itself
(`ironsecco.github.io`), meaning humans are arriving from the published docs, not just the
repo. `Google` also shows up, an early sign the SEO work (IRO-277) is getting indexed. No
announcement has fired (IRO-40 still gated), so every one of these is pre-launch trickle.
Discussions remain seeded-only: 6 threads, 0 organic external threads, 0 comments.

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

## Contributor on-ramp surface (evergreen)

The pieces a prospective contributor lands on, kept alive independently of the launch gate:

- **Good first issues:** 16 open and scoped, with labels + acceptance criteria + file pointers.
  IRO-209 seeded 8 (#108, #109, #113, #191, #192, #193, #223, #224); IRO-263 added 5
  (#281 seccomp tests, #282 contract-enum tests, #283 `cmd/ironctl` `doc.go`, #284 stale-comment
  fix, #285 tabwriter constants); IRO-286 added 3 more from the fresh surface area
  (#303 supported-providers reference table, #304 `examples/` capability matrix, #305 CI
  status badges for the functional smoke workflows). Spread across sandbox / control-plane /
  cli / docs / ci.
- **Discussions:** the 3 seeds (#116, #225, #226) plus 3 evergreen technical Q&A entries added by
  IRO-263, sourced from the threat model + red-team harness (#270 how isolation works, #271 what
  the red-team harness proves, #272 where keys live). These are contributor/technical content,
  distinct from the launch-gated announcement copy (IRO-186). **Triage (2026-07-02):** all 6
  threads reviewed — 0 organic/external threads and 0 comments, so nothing to answer or close;
  the self-authored Q&A entries carry their full answers inline. Nothing actionable until real
  visitors post.

`goodfirstissues.com` already shows up as a referrer in the snapshot below, so the GFI surface is
the honest early on-ramp worth growing pre-launch.

## Related

- Launch engagement playbook: [launch-engagement-playbook.md](launch-engagement-playbook.md)
- Launch gate / readiness: tracked in IRO-40
- Directory & awesome-list discoverability: IRO-177, IRO-98
