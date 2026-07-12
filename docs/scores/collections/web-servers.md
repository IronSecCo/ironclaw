---
title: "Most secure web server and reverse proxy container images, ranked by isolation score"
description: "Ranked isolation scores for 16 web server and proxy container images, graded 0-100 by ironctl scan. Best 63/100, average 53/100. See which ship hardened."
---

# Most secure web server and reverse proxy container images, ranked by isolation score

How isolated are the most-pulled **web server and proxy** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **16 web servers, reverse proxies, and load balancers (nginx, Apache, Caddy, HAProxy, Traefik)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 63/100** (average **53/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No web server and proxy image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`haproxy:3.1-alpine`](../haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`jetty:12-jre21`](../jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`kong:3.8`](../kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`nginxinc/nginx-unprivileged:1.27-alpine`](../nginx-unprivileged.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`varnish:7.6-alpine`](../varnish.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`caddy:2-alpine`](../caddy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 7 | [`envoyproxy/envoy:v1.32-latest`](../envoy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 8 | [`httpd:2.4-alpine`](../httpd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 9 | [`nginx:1.27-alpine`](../nginx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`nginxproxy/nginx-proxy:1.6.4`](../nginx-proxy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`jc21/nginx-proxy-manager:2.12.1`](../nginx-proxy-manager.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`openresty/openresty:alpine`](../openresty.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`tomcat:10.1-jre21-temurin`](../tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`tomee:9.1.3-jre17`](../tomee.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 15 | [`traefik:v3.2`](../traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 16 | [`unit:1.34.1-minimal`](../unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own web server and proxy container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the web server and proxy images ranked above:

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
