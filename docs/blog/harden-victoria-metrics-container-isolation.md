---
title: "How to harden a VictoriaMetrics container: victoria-metrics:v1.107.0 scores 48/100 by default"
description: "victoria-metrics:v1.107.0 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden a VictoriaMetrics container (and is victoria-metrics:v1.107.0 safe for untrusted workloads?)

Short answer: a stock `docker run victoriametrics/victoria-metrics:v1.107.0` is **not** a boundary you
should trust around untrusted code or an untrusted network. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is safer.
Four runtime flags take the same image to **100 of 100, grade A**. This guide shows the exact gaps and
the exact fixes, straight from the scan data.

> Every number here comes from a read-only `docker inspect` of
> `victoriametrics/victoria-metrics:v1.107.0`, the same data behind its
> [isolation scorecard](../scores/victoria-metrics.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run victoriametrics/victoria-metrics:v1.107.0`, four of them fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For a time-series database that holds every metric your fleet emits, the two that should worry you
most are **egress** and **root**. A VictoriaMetrics process that can reach the network is one that can
exfiltrate your entire metrics history the moment it is compromised (a poisoned remote-write payload,
a MetricsQL CVE). And a root process that escapes the container escapes as root on the host.

## Harden it: the exact `--fix` remediation

`ironctl scan my-victoria-metrics --fix` prints one remediation per failed dimension, then a single
copy-pasteable hardened run. For `victoriametrics/victoria-metrics:v1.107.0`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. VictoriaMetrics needs none of the defaults.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the storage directory at a volume this uid owns.
- **`--network=none`** (Network isolation, +11): if the TSDB is only reached by services on a private
  user-defined network, cut host egress entirely; otherwise attach a single internal network with no
  default route, not `bridge`.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/victoria-metrics-data` as an explicit writable volume. Removes the persistence surface.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name victoria-metrics \
  victoriametrics/victoria-metrics:v1.107.0

# After: 100/100, grade A
docker run -d --name victoria-metrics-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v vmdata:/victoria-metrics-data \
  --network=none \
  victoriametrics/victoria-metrics:v1.107.0
```

Rescan and the same seven dimensions all pass: `ironctl scan victoria-metrics-hardened` reports
`100/100 grade A`. That is a **52-point swing from four one-line flags**, no image rebuild.

`--network=none` fits a store that is queried only by co-located services. If a fleet of agents or
`vmagent` instances pushes metrics in over the network, keep the node hardened but hold the network
dimension at a WARN with a scoped internal network instead. That is an honest **89/100, grade B**
ceiling: the last 11 points pay for cutting the remote-write connection the fleet needs.

## Verify it on your own database

The grade above is the default image. Your deployment is what matters. Scan it in ten seconds:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-victoria-metrics
ironctl scan my-victoria-metrics --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the VictoriaMetrics in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [victoria-metrics:v1.107.0 isolation scorecard &rarr;](../scores/victoria-metrics.md): the full dimension breakdown.
- [Observability tools, ranked by isolation &rarr;](../scores/collections/observability.md): how VictoriaMetrics compares to Prometheus, InfluxDB, Loki, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
