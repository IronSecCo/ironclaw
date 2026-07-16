---
title: "How to harden a CouchDB container: couchdb:3.4 scores 48/100 by default"
description: "couchdb:3.4 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a co-located document database to a full 100/100 grade A."
---

# How to harden a CouchDB container (and is couchdb:3.4 safe for your documents?)

CouchDB stores JSON documents that an application layer reads and writes all day: user profiles,
configuration, offline-sync state, whatever your app persists. A stock `docker run couchdb:3.4` keeps
that store behind a boundary weaker than the data deserves. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is safer.
Unlike a broker or a proxy, a document database that only its co-located application talks to can
close every dimension, including the network. A few runtime flags take the same image to a full
**100 of 100, grade A**. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `couchdb:3.4`, the same data behind its
> [isolation scorecard](../scores/couchdb.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run couchdb:3.4`,
three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and **egress**. A CouchDB process that escapes as root
escapes as root on the host, right next to the `.couch` files it was serving. And a database that can
reach arbitrary destinations is one that can quietly ship every document out the moment an HTTP-API or
Erlang CVE lands code execution. The default capability set and writable rootfs widen and entrench
that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-couchdb --fix` prints one remediation per failed dimension, then one hardened run. For
`couchdb:3.4`:

- **`--user 5984:5984`** (Non-root user, +15): pin the non-root `couchdb` uid so an escape does not
  begin as host uid 0. Point the data directory at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; CouchDB needs none of
  the default set to serve its HTTP API on a high port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/opt/couchdb/data` as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  store can actually max out. If the only client is an application on the same host or pod reaching
  CouchDB over the loopback of a shared network namespace, cut the NIC entirely. Nothing external can
  connect, and the database cannot phone home.

### When network=none is not honest

If remote clients, Fauxton browser users, or a CouchDB cluster with peer connections between nodes
need the network, you cannot use `--network=none`; the store has to accept those connections. In that
case put it on a user-defined network scoped to just its clients and cluster peers, with no default
route out. That holds the network dimension at a WARN (4 of 15) and the honest ceiling becomes
**89 of 100, grade B**, the same as a broker. Use `--network=none` only for the single-application,
co-located case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name couchdb couchdb:3.4

# After: 100/100, grade A (co-located store, no network needed)
docker run -d --name couchdb-hardened \
  --user 5984:5984 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v couchdb-data:/opt/couchdb/data \
  --network=none \
  couchdb:3.4
```

Rescan: `ironctl scan couchdb-hardened` reports `100/100 grade A`. A **52-point swing** with no custom
image build, just the right flags. Every dimension is closed because a co-located document database
does not need to talk to anything but the app on the other side of its loopback. That is the top
grade, reserved for datastores whose clients live next to them.

## Verify it on your own CouchDB

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-couchdb
ironctl scan my-couchdb --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade the
CouchDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [couchdb:3.4 isolation scorecard &rarr;](../scores/couchdb.md): the full dimension breakdown.
- [How to harden a MongoDB container &rarr;](harden-mongodb-container-isolation.md): another document store that reaches grade A when co-located.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
