---
title: "How to harden a Cassandra container: cassandra:5.0 scores 48/100 by default"
description: "cassandra:5.0 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden a Cassandra container (and is cassandra:5.0 safe for untrusted workloads?)

Short answer: a stock `docker run cassandra:5.0` is **not** a boundary you should trust around
untrusted code or an untrusted network. Graded on IronClaw's seven-dimension containment scale,
the default configuration scores **48 of 100, grade D (porous)**. Higher is safer. Four runtime
flags take the same image to **100 of 100, grade A**. This guide shows the exact gaps and the
exact fixes, straight from the scan data.

> Every number here comes from a read-only `docker inspect` of `cassandra:5.0`, the same data behind
> its [isolation scorecard](../scores/cassandra.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
cassandra:5.0`, four of them fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For a wide-column database, the two that should worry you most are **egress** and **root**. A
Cassandra process that can reach the network is one that can exfiltrate every keyspace it holds the
moment it is compromised (a deserialization CVE, a poisoned UDF, a driver bug). And a root process
that escapes the container escapes as root on the host.

## Harden it: the exact `--fix` remediation

`ironctl scan my-cassandra --fix` prints one remediation per failed dimension, then assembles a
single copy-pasteable hardened run. For `cassandra:5.0` the prescription is:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Cassandra itself needs none of the defaults.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the data directory at a volume this uid owns.
- **`--network=none`** (Network isolation, +11): a single-node Cassandra reached only by co-located
  services can cut host egress entirely; otherwise attach one internal network with no default route,
  not `bridge`. See the cluster note below.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/var/lib/cassandra` as an explicit writable volume. Removes the persistence surface.

### The cluster note

A multi-node Cassandra ring uses the gossip protocol between nodes, so those nodes must reach each
other. `--network=none` fits a single-node instance that only its co-located app talks to. For a
real cluster, hold the network dimension at a WARN instead: attach a user-defined network scoped to
just the ring members and their client apps, with no default route out, so a compromised node cannot
call arbitrary internet addresses. That posture scores the **89/100, grade B** ceiling the network
services on this set share; the 100/A run below is the single-node case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name cassandra cassandra:5.0

# After: 100/100, grade A (single-node, private data volume)
docker run -d --name cassandra-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v cassandra-data:/var/lib/cassandra \
  --network=none \
  cassandra:5.0
```

Rescan and the same seven dimensions all pass: `ironctl scan cassandra-hardened` reports
`100/100 grade A`. That is a **52-point swing from four one-line flags**, no image rebuild.

## Verify it on your own database

The grade above is the default image. Your deployment is what matters. Scan it in ten seconds:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-cassandra
ironctl scan my-cassandra --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Cassandra in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [cassandra:5.0 isolation scorecard &rarr;](../scores/cassandra.md): the full dimension breakdown.
- [Databases, ranked by isolation &rarr;](../scores/collections/databases.md): how Cassandra compares to Postgres, MongoDB, Redis, and the rest.
- [How to harden a ClickHouse container &rarr;](harden-clickhouse-container-isolation.md): the same walkthrough for the analytics side.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
