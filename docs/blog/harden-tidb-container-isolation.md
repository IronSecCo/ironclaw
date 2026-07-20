---
title: "How to harden a TiDB container: pingcap/tidb:v8.5.0 scores 48/100 by default"
description: "pingcap/tidb:v8.5.0 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a distributed SQL database to 100/100 grade A."
---

# How to harden a TiDB container (and is pingcap/tidb:v8.5.0 safe for untrusted workloads?)

TiDB is a distributed SQL database that speaks the MySQL protocol, so its container fronts your
application's queries and belongs among the tightest things you run. A stock `docker run
pingcap/tidb:v8.5.0` is not: graded on IronClaw's seven-dimension containment scale it scores **48 of
100, grade D (porous)**. Higher is safer. A short list of runtime flags takes the same image to **100
of 100, grade A** on a single-instance deployment whose components share one private network. Here
are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `pingcap/tidb:v8.5.0`, the same data
> behind its [isolation scorecard](../scores/tidb.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
pingcap/tidb:v8.5.0`, four fail or warn:

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
SQL-parser or plugin CVE that lands code execution lands it as uid 0, with `CAP_NET_RAW` and friends,
on a filesystem it can rewrite to persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-tidb --fix` prints one remediation per failed dimension, then one hardened run.
For `pingcap/tidb:v8.5.0`:

- **`--user 65532:65532`** (Non-root, +15): run the tidb-server process as an unprivileged uid.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. The SQL layer needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): the tidb-server is stateless (storage lives
  in TiKV), so a read-only rootfs with a `/tmp` scratch mount removes the persistence surface cleanly.
- **`--network=none`** (Network isolation, +11): when the whole stack (PD, TiKV, tidb-server) sits on
  one private compose network with no default route out, the tidb-server needs no host egress. Cutting
  it is the dimension that takes a single-machine deployment to a full A.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name tidb pingcap/tidb:v8.5.0

# After: 100/100, grade A
docker run -d --name tidb-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  pingcap/tidb:v8.5.0
```

Rescan: `ironctl scan tidb-hardened` reports `100/100 grade A`. A **52-point swing** with no custom
image build, just the right flags.

> **Running a production cluster?** A multi-node TiDB where tidb-server reaches PD and TiKV across
> hosts, or where remote MySQL clients connect, needs a real network, so `--network=none` is wrong
> there. Keep the other four fixes and attach a scoped private network; the honest ceiling is then
> **89/100, grade B**, with the network dimension held at a WARN. A single-machine stack behind one
> private network reaches the full A.

## Verify it on your own database

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-tidb
ironctl scan my-tidb --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the TiDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [tidb:v8.5.0 isolation scorecard &rarr;](../scores/tidb.md): the full dimension breakdown.
- [Databases, ranked by isolation &rarr;](../scores/collections/databases.md): how TiDB compares to CockroachDB, MySQL, and the rest.
- [How to harden a CockroachDB container &rarr;](harden-cockroachdb-container-isolation.md): another distributed SQL database with the same single-node A and clustered-B story.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
