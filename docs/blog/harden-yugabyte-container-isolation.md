---
title: "How to harden a YugabyteDB container: yugabyte scores 48/100 by default"
description: "yugabyte defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a distributed SQL store to 100/100 grade A."
---

# How to harden a YugabyteDB container (and is yugabyte safe in your stack?)

YugabyteDB is your system of record: a distributed SQL store holding the tables your application
cannot lose. A stock `docker run yugabytedb/yugabyte` is not the boundary that role deserves. Graded
on IronClaw's seven-dimension containment scale, the default configuration scores
**48 of 100, grade D (porous)**. Higher is safer. A few runtime flags take the same image to
**100 of 100, grade A** for a single-node store only its co-located app talks to, or an honest
**89 of 100, grade B** for a multi-node cluster where peers must reach each other. Here are the exact
gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `yugabytedb/yugabyte:2.23.0.0-b710`,
> the same data behind its [isolation scorecard](../scores/yugabyte.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run yugabytedb/yugabyte`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**. A distributed database holds every row your app trusts
and the credentials that reach it; a container escape from a root process lands as root on the host,
next to that data. The full capability set widens the foothold and the writable rootfs makes it
durable.

## Harden it: the exact `--fix` remediation

`ironctl scan my-yugabyte --fix` prints one remediation per failed dimension, then one hardened run.
For `yugabyte`:

- **`--user 1000:1000`** (Non-root user, +15): run as a non-root uid so an escape does not begin as
  host uid 0. Point the data directory at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +20): drop every Linux capability; the SQL and RPC
  listeners bind high ports and need none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  the data directory as an explicit writable volume. Removes the persistence surface.
- **`--network=none` (single node) or a scoped network (cluster)** (Network isolation): a single-node
  YugabyteDB that only its co-located app talks to can take `--network=none` and score the full 15,
  reaching **100/100**. A multi-node cluster cannot: tservers and masters must reach each other, so the
  network holds at a WARN (4 of 15) and the honest ceiling is **89/100, grade B**. Scope the cluster
  to a private network with no default route out.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name yugabyte \
  yugabytedb/yugabyte:2.23.0.0-b710 \
  bin/yugabyted start --daemon=false

# After: 100/100, grade A (single node, only its app connects)
docker run -d --name yugabyte-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v yugabyte-data:/home/yugabyte/var \
  --network=none \
  yugabytedb/yugabyte:2.23.0.0-b710 \
  bin/yugabyted start --daemon=false
```

Rescan reports `100/100 grade A`, a **52-point swing** with no custom image build, just the right
flags. Proven directly on the hardened config with `ironctl scan --compose`:

```
score:   100/100  grade A  (hardened)
Non-root user (uid != 0)    [+] PASS  15/15  runs as 1000:1000 (uid != 0)
Dropped capabilities        [+] PASS  20/20  all capabilities dropped, none added back
Read-only root filesystem   [+] PASS  10/10  root filesystem is read-only
Network isolation / egress  [+] PASS  15/15  network=none: no NIC but loopback, no egress
```

For a multi-node cluster, drop the `--network=none` for a scoped private network shared only with the
other nodes; the grade settles at its honest **89/100, grade B** and we say so rather than inflate it.

## Verify it on your own YugabyteDB

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-yugabyte
ironctl scan my-yugabyte --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the YugabyteDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [yugabyte isolation scorecard &rarr;](../scores/yugabyte.md): the full dimension breakdown.
- [How to harden a CockroachDB container &rarr;](harden-cockroachdb-container-isolation.md): the other distributed SQL store, same pattern.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
