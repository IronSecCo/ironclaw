---
title: "How to harden a Grafana container: grafana/grafana:11.2.0 scores 63/100 by default"
description: "grafana/grafana:11.2.0 defaults score 63/100 (grade C): full caps and a writable rootfs. The exact ironctl scan --fix flags that take a dashboard server to its honest 89/100 grade B."
---

# How to harden a Grafana container (and is grafana/grafana:11.2.0 safe next to your metrics?)

Grafana is the window into every other system you run: it holds datasource credentials, query
access to your metrics and logs, and often SSO tokens for the humans who log in. A stock
`docker run grafana/grafana:11.2.0` gets one thing right and three things wrong. Graded on
IronClaw's seven-dimension containment scale, the default configuration scores **63 of 100, grade C
(weak)**. Higher is safer. The image already runs as a non-root uid, which is more than most images
on this directory manage, but it keeps the full capability set and a writable root filesystem. A few
runtime flags take the same image to **89 of 100, grade B**, one point off an A. The one dimension it
cannot reach is the one a dashboard server needs by definition: browsers have to connect to it. Here
are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `grafana/grafana:11.2.0`, the same data
> behind its [isolation scorecard](../scores/grafana.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
grafana/grafana:11.2.0`, two fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as 472 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **capabilities** and **egress**. Grafana renders untrusted
input: dashboard JSON, plugin code, and datasource responses all flow through it, and its plugin
system runs backend binaries. A rendering or plugin CVE that lands code execution inherits
`CAP_NET_RAW` and the rest of the default set, and from a container that can reach arbitrary
destinations it can quietly ship your datasource credentials and query results out. The writable
rootfs is the persistence surface that makes such a foothold durable.

## Harden it: the exact `--fix` remediation

`ironctl scan my-grafana --fix` prints one remediation per failed dimension, then one hardened run.
For `grafana/grafana:11.2.0`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability. Grafana needs none
  of the default set to serve dashboards; it binds to a high port by default.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount the data path (`/var/lib/grafana`) as an explicit writable volume this uid owns. Removes the
  tamper and persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  dashboard server, browsers and its datasources must reach it. Any named or bridge network scores 4
  of 15 (a WARN, not a fail): a connection path exists. Contain it anyway: put Grafana on a
  user-defined network scoped to just the reverse proxy in front of it and the datasources behind it,
  with no default route out, so a compromised Grafana cannot call arbitrary internet addresses.

The uid is already non-root, so the non-root dimension is a PASS out of the box. Keep it that way:
point the data volume at the `472` uid rather than reverting to root to dodge a permissions error.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name grafana grafana/grafana:11.2.0

# After: 89/100, grade B (scoped private network for browsers and datasources)
docker run -d --name grafana-hardened \
  --user 472:472 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v grafana-data:/var/lib/grafana \
  --network=observability-internal \
  grafana/grafana:11.2.0
```

Rescan: `ironctl scan grafana-hardened` reports `89/100 grade B`. A **26-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a dashboard server exists to be connected to; `network=none` would score the last points
but leave no browser able to load a panel. That is the honest ceiling for a UI service, and it is a
clear step up from the default C.

## Verify it on your own Grafana

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-grafana
ironctl scan my-grafana --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Grafana in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [grafana:11.2.0 isolation scorecard &rarr;](../scores/grafana.md): the full dimension breakdown.
- [How to harden a Prometheus container &rarr;](harden-prometheus-container-isolation.md): the metrics store Grafana usually queries, with the same honest ceiling.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
