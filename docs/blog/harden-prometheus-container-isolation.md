---
title: "How to harden a Prometheus container: prom/prometheus:v3.1.0 scores 63/100 by default"
description: "prom/prometheus:v3.1.0 defaults score 63/100 (grade C): full caps and a writable rootfs. The exact ironctl scan --fix flags that take a metrics server to its honest 89/100 grade B."
---

# How to harden a Prometheus container (and is prom/prometheus:v3.1.0 safe for your cluster?)

Prometheus is the monitoring brain of most clusters: it scrapes every target you run, holds a
time-series record of how the whole system behaves, and often carries the alerting rules that page
your team. That reach makes it a high-value target, and a stock `docker run prom/prometheus:v3.1.0`
does not fully earn the trust the role implies. Graded on IronClaw's seven-dimension containment
scale, the default configuration scores **63 of 100, grade C (weak)**. Higher is safer. The image
runs as the `nobody` uid out of the box, which is better than most, but it keeps the full capability
set and a writable root filesystem. A few runtime flags take the same image to **89 of 100, grade
B**, one point off an A. The one dimension it cannot reach is the one a metrics server needs by
definition: it has to scrape targets and serve its API. Here are the exact gaps and fixes from the
scan data.

> Every number here comes from a read-only `docker inspect` of `prom/prometheus:v3.1.0`, the same data
> behind its [isolation scorecard](../scores/prometheus.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
prom/prometheus:v3.1.0`, two fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as nobody (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **capabilities** and **egress**. Prometheus already reaches
out across your network by design, so an egress-capable Prometheus that gets code execution through a
scrape-target parsing bug or an exporter CVE can pivot anywhere it can already route, and exfiltrate
the full time-series history of your infrastructure. The default capability set gives such a foothold
`CAP_NET_RAW` for raw-socket tricks, and the writable rootfs lets it persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-prometheus --fix` prints one remediation per failed dimension, then one hardened
run. For `prom/prometheus:v3.1.0`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability. Prometheus scrapes
  over ordinary TCP and binds a high port; it needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount the TSDB path (`/prometheus`) as an explicit writable volume the `nobody` uid owns. Removes
  the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  monitoring server, it has to reach its scrape targets and serve its query API. Any named or bridge
  network scores 4 of 15 (a WARN, not a fail): a connection path exists. Contain it anyway: attach a
  user-defined network scoped to just the exporters it scrapes and the Grafana or Alertmanager that
  read from it, with no default route out, so a compromised Prometheus cannot call arbitrary internet
  addresses.

The uid is already non-root, so that dimension is a PASS out of the box. Keep it that way: own the
TSDB volume with the `nobody` uid rather than reverting to root to sidestep a permissions error.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name prometheus prom/prometheus:v3.1.0

# After: 89/100, grade B (scoped private network for scrape targets and readers)
docker run -d --name prometheus-hardened \
  --user nobody \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v prometheus-data:/prometheus \
  --network=observability-internal \
  prom/prometheus:v3.1.0
```

Rescan: `ironctl scan prometheus-hardened` reports `89/100 grade B`. A **26-point swing** with no
custom image build, just the right flags. The only dimension still short of full marks is the network
(4 of 15), because a monitoring server exists to reach out and be queried; `network=none` would score
the last points but leave nothing to scrape and no API to serve. That is the honest ceiling for a
metrics server, and it is a clear step up from the default C.

## Verify it on your own Prometheus

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-prometheus
ironctl scan my-prometheus --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Prometheus in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [prometheus:v3.1.0 isolation scorecard &rarr;](../scores/prometheus.md): the full dimension breakdown.
- [How to harden a Grafana container &rarr;](harden-grafana-container-isolation.md): the dashboard that usually sits in front of Prometheus, with the same honest ceiling.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
