---
title: "We graded 151 popular Docker images for isolation. Not one scored an A."
description: "The Container Isolation Leaderboard ranks 151 of the most-pulled public Docker images by how contained they are on plain docker run defaults, graded 0 to 100 by ironctl scan. Scores run 48 to 63 out of 100. Here are the Hall of Fame leaders, the worst offenders, and the three gaps that sink almost every image."
---

# We graded 151 popular Docker images for isolation. Not one scored an A.

Pull any popular image, run it the way its own README tells you to, and hand it
some untrusted code. How much isolation are you actually getting between that
code and your host? Most teams cannot answer with a number.

So we measured it, across the whole ecosystem. We took **151 of the
most-pulled public Docker images**, ran each one in its default configuration,
and graded it 0 to 100 on how contained it is, using
[`ironctl scan`](audit-your-sandbox-in-10-seconds.md). The full ranking is the
[Container Isolation Leaderboard](../scores/leaderboard.md), regenerated weekly
so it never goes stale.

The headline is uncomfortable: **no popular image ships isolated by default.**
Scores run from **48 to 63 out of 100**, with an average of **52**. The best
image in the survey earns a C. The worst earns a D. Not a single one reaches a
clean 100 out of 100 grade A, and the gap between any of them and an A is a
handful of `docker run` flags.

## How the grade works

`ironctl scan` inspects a container or image and scores seven containment
dimensions: dropped capabilities, non-root user, read-only root filesystem,
network isolation and egress, seccomp profile, no privileged mode, and no
dangerous host mounts. Each failing dimension names the exact hole and the flag
that closes it. No account, no cloud, no agent to install. You can run the same
scan on your own images in about ten seconds:

```bash
ironctl scan nginx:latest
```

Every number below comes from that scan, run over the pinned image manifest in
[`examples/isolation-survey`](https://github.com/IronSecCo/ironclaw/tree/main/examples/isolation-survey).
If you disagree with a grade, re-run it yourself.

## Hall of Fame: the best of a weak field

No image ships at grade A, so the leaders are the ones that start you closest to
a hardened posture. Every one of these lands at **63 out of 100, a grade C**,
held back only by capabilities, egress, and a writable root filesystem:

| Rank | Image | Score | Grade |
|-----:|-------|------:|:-----:|
| 1 | `adminer:4.8.1` | 63/100 | C |
| 2 | `prom/alertmanager:v0.28.0` | 63/100 | C |
| 3 | `confluentinc/cp-kafka:7.8.0` | 63/100 | C |
| 4 | `grafana/grafana:11.4.0` | 63/100 | C |
| 5 | `memcached:1.6-alpine` | 63/100 | C |

What earns these images their edge is a single good default the rest of the
field skips: they do not run as root. That one choice is worth 15 points.

## Worst offenders: one step from host uid 0

At the bottom, a cluster of images all score **48 out of 100, a grade D**. Run
one of these unhardened and a container escape lands one step from root on your
host:

| Image | Score | Grade | The gaps |
|-------|------:|:-----:|----------|
| `zookeeper:3.9` | 48/100 | D | root user, full caps, open egress |
| `wordpress:6-php8.3-apache` | 48/100 | D | root user, full caps, open egress |
| `ubuntu:24.04` | 48/100 | D | root user, full caps, open egress |
| `traefik:v3.2` | 48/100 | D | root user, full caps, open egress |
| `hashicorp/vault:1.18` | 48/100 | D | root user, full caps, open egress |

These are not misconfigured or exotic. They are images you already run, started
exactly the way their quickstart tells you to.

## The same three holes, almost every time

Across all 151 images, the failures cluster in the same places:

1. **Full Linux capabilities.** Almost nothing drops caps by default, so a
   container keeps a broad set of kernel privileges it never uses.
2. **Open egress.** The default bridge network lets a compromised container
   phone home or reach your internal services.
3. **Writable root filesystem.** Nothing stops a payload from rewriting binaries
   and libraries inside the container.

The worst tier adds a fourth: **running as root (uid 0)**. The images that climb
into the Hall of Fame are, almost without exception, the ones that drop that one
habit.

None of this needs a different runtime or a rebuild. The exact flags that move an
image from a D to an A ship on every scorecard, and `ironctl scan --fix` writes
them for you.

## See where your images land

Browse the full interactive ranking, filter by category, and pull a copy-paste
score badge for your own repo at the
**[Container Isolation Scores explorer](https://nivardsec.com/scores)**. Every
image links to a per-dimension scorecard with the precise hardening flags. The
[leaderboard](../scores/leaderboard.md) and the underlying
[scores directory](../scores/index.md) live in the docs and refresh weekly.

If the image you run is not in the survey yet, scan it in ten seconds and see
your own number:

```bash
ironctl scan your-image:tag
```
