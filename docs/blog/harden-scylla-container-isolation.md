---
title: "How to harden a ScyllaDB container: scylla:6.2 scores 48/100 by default"
description: "scylla:6.2 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden a ScyllaDB container (and is scylla:6.2 safe for untrusted workloads?)

Short answer: a stock `docker run scylladb/scylla:6.2` is **not** a boundary you should trust
around untrusted code or an untrusted network. Graded on IronClaw's seven-dimension containment
scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is safer. Four
runtime flags take the same image to **100 of 100, grade A**. This guide shows the exact gaps and
the exact fixes, straight from the scan data.

> Every number here comes from a read-only `docker inspect` of `scylladb/scylla:6.2`, the same
> data behind its [isolation scorecard](../scores/scylla.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run scylladb/scylla:6.2`, four of them fail or warn:

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
Scylla process that can reach the network is a Scylla process that can exfiltrate every row it
stores the moment it is compromised (a poisoned UDF, a CQL or driver CVE). And a root process that
escapes the container escapes as root on the host.

## Harden it: the exact `--fix` remediation

`ironctl scan my-scylla --fix` prints one remediation per failed dimension, then a single
copy-pasteable hardened run. For `scylladb/scylla:6.2`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only
  what the workload provably needs. Scylla itself needs none of the defaults.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the data directory at a volume this uid owns.
- **`--network=none`** (Network isolation, +11): for a single-node store reached only by
  co-located services on a private user-defined network, cut host egress entirely; otherwise
  attach a single internal network with no default route, not `bridge`. (A multi-node cluster
  needs inter-node gossip on a scoped private network, which is a WARN, not a clean pass, see the
  ceiling note below.)
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/var/lib/scylla` as an explicit writable volume. Removes the persistence surface.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name scylla scylladb/scylla:6.2

# After: 100/100, grade A (single-node)
docker run -d --name scylla-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v scylla-data:/var/lib/scylla \
  --network=none \
  scylladb/scylla:6.2
```

Rescan and the same seven dimensions all pass: `ironctl scan scylla-hardened` reports
`100/100 grade A`. That is a **52-point swing from four one-line flags**, no image rebuild.

## The honest ceiling for a multi-node cluster

The 100/100 above is a **single-node** store with `network=none`. A multi-node Scylla cluster
cannot use `network=none`: nodes gossip and stream data to each other. Attach a private
user-defined network scoped to just the cluster with no default route out, and the network
dimension scores 4 of 15 (a WARN, not a fail) instead of the full 15. That puts a hardened
multi-node cluster at **89 of 100, grade B**, one point off an A. The other six dimensions still
max out. That is the honest ceiling when nodes must talk, and it is a long way from the default D.

## Verify it on your own database

The grade above is the default image. Your deployment is what matters. Scan it in ten seconds:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-scylla
ironctl scan my-scylla --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the Scylla in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [scylla:6.2 isolation scorecard &rarr;](../scores/scylla.md): the full dimension breakdown.
- [Databases, ranked by isolation &rarr;](../scores/collections/databases.md): how Scylla compares to Cassandra, MongoDB, Postgres, and the rest.
- [How to harden a Cassandra container &rarr;](harden-cassandra-container-isolation.md): the same walkthrough for the wide-column database Scylla is compatible with.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
