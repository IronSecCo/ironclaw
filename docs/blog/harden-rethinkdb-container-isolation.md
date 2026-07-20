---
title: "How to harden a RethinkDB container: rethinkdb:2.4 scores 48/100 by default"
description: "rethinkdb:2.4 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a realtime document database to 100/100 grade A."
---

# How to harden a RethinkDB container (and is rethinkdb:2.4 safe for untrusted workloads?)

A realtime document database pushes query results straight to connected clients, so the process sits
close to your application data and deserves a tight container. A stock `docker run rethinkdb:2.4` is
not: graded on IronClaw's seven-dimension containment scale it scores **48 of 100, grade D
(porous)**. Higher is safer. A short list of runtime flags takes the same image to **100 of 100,
grade A**, because a single-node RethinkDB that only its co-located app queries can drop its network
entirely. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `rethinkdb:2.4`, the same data behind
> its [isolation scorecard](../scores/rethinkdb.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
rethinkdb:2.4`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The sharpest edges are **root**, the **retained capabilities**, and the **writable rootfs**: a
ReQL-parser or driver CVE that lands code execution lands it as uid 0, with `CAP_NET_RAW` and
friends, on a filesystem it can rewrite to persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-rethinkdb --fix` prints one remediation per failed dimension, then one hardened run.
For `rethinkdb:2.4`:

- **`--user 65532:65532`** (Non-root, +15): run the process as an unprivileged uid. Point the data
  directory (`/data`) at a volume that uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. RethinkDB needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/data` as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11): a single-node document database that only its
  co-located app queries needs no external network at all. Attach it to app services over a private
  compose network and cut the default route out. This is the dimension that takes it to a full A.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name rethinkdb rethinkdb:2.4

# After: 100/100, grade A
docker run -d --name rethinkdb-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v rethinkdb-data:/data \
  --network=none \
  rethinkdb:2.4
```

Rescan: `ironctl scan rethinkdb-hardened` reports `100/100 grade A`. A **52-point swing** with no
custom image build, just the right flags.

> **Running a cluster?** A multi-node RethinkDB (peers joined over the cluster port, or remote
> drivers) needs the nodes to reach each other, so `--network=none` is wrong there. Keep the other
> four fixes and attach a scoped private network; the honest ceiling is then **89/100, grade B**, with
> the network dimension held at a WARN. Single-node with a co-located app reaches the full A.

## Verify it on your own database

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-rethinkdb
ironctl scan my-rethinkdb --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the RethinkDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [rethinkdb:2.4 isolation scorecard &rarr;](../scores/rethinkdb.md): the full dimension breakdown.
- [Databases, ranked by isolation &rarr;](../scores/collections/databases.md): how RethinkDB compares to MongoDB, CouchDB, and the rest.
- [How to harden a MongoDB container &rarr;](harden-mongodb-container-isolation.md): another document store with the same single-node A and clustered-B story.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
