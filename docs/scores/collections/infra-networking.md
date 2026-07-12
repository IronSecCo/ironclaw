---
title: "Most secure infrastructure and networking container images, ranked by isolation score"
description: "Ranked isolation scores for 8 infrastructure and networking container images, graded 0-100 by ironctl scan. Average 48/100."
---

# Most secure infrastructure and networking container images, ranked by isolation score

How isolated are the most-pulled **infrastructure and networking** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **8 service discovery, secrets, orchestration, and networking (Consul, Vault, Nomad, k3s)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 48/100** (average **48/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No infrastructure and networking image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`hashicorp/consul:1.20`](../consul.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 🥈 2 | [`docker:27-dind`](../docker.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 🥉 3 | [`rancher/k3s:v1.31.4-k3s1`](../k3s.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 4 | [`hashicorp/nomad:1.9`](../nomad.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 5 | [`registry:2.8.3`](../registry.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 6 | [`tailscale/tailscale:v1.78.3`](../tailscale.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 7 | [`hashicorp/vault:1.18`](../vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 8 | [`lscr.io/linuxserver/wireguard:1.0.20210914`](../wireguard.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own infrastructure and networking container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the infrastructure and networking images ranked above:

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
