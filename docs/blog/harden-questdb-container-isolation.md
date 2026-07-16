---
title: "How to harden a QuestDB container: questdb:8.2.1 scores 48/100 by default"
description: "questdb:8.2.1 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a co-located time-series database to a full 100/100 grade A."
---

# How to harden a QuestDB container (and is questdb:8.2.1 safe for your metrics?)

QuestDB ingests high-throughput time-series data: sensor readings, financial ticks, application
metrics, the append-only history you query for trends. A stock `docker run questdb/questdb:8.2.1` keeps
that store behind a boundary weaker than the data deserves. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is safer.
Unlike a broker or a proxy, a time-series database that only its co-located application writes to can
close every dimension, including the network. A few runtime flags take the same image to a full
**100 of 100, grade A**. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `questdb:8.2.1`, the same data behind
> its [isolation scorecard](../scores/questdb.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run questdb/questdb:8.2.1`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and **egress**. A QuestDB process that escapes as root
escapes as root on the host, right next to the column files it was serving. And a database that can
reach arbitrary destinations is one that can quietly exfiltrate your entire history the moment an
ingestion-protocol or HTTP-API CVE lands code execution. The default capability set and writable
rootfs widen and entrench that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-questdb --fix` prints one remediation per failed dimension, then one hardened run. For
`questdb:8.2.1`:

- **`--user 10001:10001`** (Non-root user, +15): pin a non-root uid so an escape does not begin as host
  uid 0. Point the data root at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; QuestDB needs none of
  the default set to serve its HTTP, PostgreSQL-wire, and InfluxDB-line ports.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/var/lib/questdb` as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  store can actually max out. If the only writer is an application on the same host or pod reaching
  QuestDB over the loopback of a shared network namespace, cut the NIC entirely. Nothing external can
  connect, and the database cannot phone home.

### When network=none is not honest

If a remote fleet pushes metrics over the network, or dashboard users hit the web console and query
API, you cannot use `--network=none`; the store has to accept those connections. In that case put it
on a user-defined network scoped to just its writers and clients, with no default route out. That
holds the network dimension at a WARN (4 of 15) and the honest ceiling becomes **89 of 100, grade B**,
the same as a broker. Use `--network=none` only for the single-application, co-located case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name questdb questdb/questdb:8.2.1

# After: 100/100, grade A (co-located store, no network needed)
docker run -d --name questdb-hardened \
  --user 10001:10001 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v questdb-data:/var/lib/questdb \
  --network=none \
  questdb/questdb:8.2.1
```

Rescan: `ironctl scan questdb-hardened` reports `100/100 grade A`. A **52-point swing** with no custom
image build, just the right flags. Every dimension is closed because a co-located time-series database
does not need to talk to anything but the app on the other side of its loopback. That is the top
grade, reserved for datastores whose clients live next to them.

## Verify it on your own QuestDB

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-questdb
ironctl scan my-questdb --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade the
QuestDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [questdb:8.2.1 isolation scorecard &rarr;](../scores/questdb.md): the full dimension breakdown.
- [How to harden an InfluxDB container &rarr;](harden-influxdb-container-isolation.md): another time-series store that reaches grade A when co-located.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
