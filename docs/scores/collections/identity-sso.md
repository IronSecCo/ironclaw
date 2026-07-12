---
title: "Most secure identity and SSO container images, ranked by isolation score"
description: "Ranked isolation scores for 4 identity and SSO container images, graded 0-100 by ironctl scan. Best 63/100, average 59/100. See which ship hardened."
---

# Most secure identity and SSO container images, ranked by isolation score

How isolated are the most-pulled **identity and SSO** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **4 identity providers and single-sign-on servers (Keycloak, Authelia, Dex, Hydra)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 63/100** (average **59/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No identity and SSO image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`dexidp/dex:v2.41.1`](../dex.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`oryd/hydra:v2.2.0`](../hydra.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`quay.io/keycloak/keycloak:26.0.7`](../keycloak.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`authelia/authelia:4.38`](../authelia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own identity and SSO container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the identity and SSO images ranked above:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own container the same way this page was generated
ironctl scan my-container
```

- [All container isolation scores &rarr;](../index.md), every scorecard, worst-isolated first.
- [Browse every category &rarr;](index.md), the full set of ranked collection pages.
- [Container Isolation Leaderboard &rarr;](../leaderboard.md), the whole dataset ranked, with a Hall of Fame and worst offenders.
- [Scan any container &rarr;](../../scan.md), the full command reference.
- [The State of Container Isolation, 2026 &rarr;](../../blog/state-of-container-isolation-2026.md), the survey these grades are built from.

---

*Part of the [Container Isolation Scores](../index.md) directory. Generated from a reproducible survey by `examples/isolation-survey/gen_scorecards.py` and refreshed weekly, so this ranking never goes stale. Grades reflect each image's default configuration, not a limit of the image itself: every one reaches grade A with the right `docker run` flags.*
