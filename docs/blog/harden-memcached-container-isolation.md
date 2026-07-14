---
title: "How to harden a Memcached container: memcached:1.6-alpine scores 63/100 by default"
description: "memcached:1.6-alpine defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take a cache to its honest 89/100 grade B."
---

# How to harden a Memcached container (and is memcached:1.6-alpine safe for untrusted workloads?)

A cache sits in the hot path of every request, which is exactly why its container should be
tight. A stock `docker run memcached:1.6-alpine` starts ahead of most images (it runs non-root
out of the box) but is still not a boundary to trust around an untrusted network. Graded on
IronClaw's seven-dimension containment scale, the default configuration scores **63 of 100,
grade C (partial)**. Higher is safer. A couple of runtime flags take the same image to **89 of
100, grade B**, one point off an A, and the one dimension it cannot reach is the one a cache
needs by definition (clients must connect to it). Here are the exact gaps and fixes from the
scan data.

> Every number here comes from a read-only `docker inspect` of `memcached:1.6-alpine`, the same
> data behind its [isolation scorecard](../scores/memcached.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run memcached:1.6-alpine`, three fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as memcache (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Memcached already runs as the non-root `memcache` user, which is why it clears the 48/100 that
root-by-default images sit at. The remaining leaks are the **retained capabilities** and the
**writable root filesystem**. Memcached is purely in-memory, it has no data directory to persist,
so there is no reason its root filesystem should be writable at all: a read-only rootfs removes
the persistence surface for free, and dropping the default capabilities takes `CAP_NET_RAW` and
friends away from any code that lands via a protocol-parsing CVE.

## Harden it: the exact `--fix` remediation

`ironctl scan my-memcached --fix` prints one remediation per failed dimension, then one hardened
run. For `memcached:1.6-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only
  what the workload provably needs. Memcached needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only.
  Memcached keeps everything in memory, so no writable volume is needed at all, a rare clean win.
- **`--user 65532:65532`** (Non-root user, already passing at +0): non-root already passes as the
  `memcache` user. Pinning an explicit fixed uid is the auditable choice; it does not change the
  score.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  cache, clients must be able to connect to it. Any named or bridge network scores 4 of 15 (a
  WARN, not a fail): a connection path exists. This is the one dimension a cache cannot max out.
  Contain it anyway: attach a user-defined network scoped to just its clients, with no default
  route out, so a compromised cache cannot call arbitrary internet addresses.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name memcached memcached:1.6-alpine

# After: 89/100, grade B (scoped private network for its clients)
docker run -d --name memcached-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=cache-internal \
  memcached:1.6-alpine
```

Rescan: `ironctl scan memcached-hardened` reports `89/100 grade B`. A **26-point swing** with no
custom image build and no volume, just the right flags. The only dimension still short of full
marks is the network (4 of 15), because a cache exists to be connected to; `network=none` would
score the last points but leave nothing able to reach it. That is the honest ceiling for a cache,
and it is a clean grade B up from the default C.

## Verify it on your own cache

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-memcached
ironctl scan my-memcached --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the Memcached in your stack, not just a bare `docker run`.

## Keep going

- [memcached:1.6-alpine isolation scorecard &rarr;](../scores/memcached.md): the full dimension breakdown.
- [Databases and caches, ranked by isolation &rarr;](../scores/collections/databases.md): how Memcached compares to Redis, Postgres, MySQL, and the rest.
- [How to harden a Redis container &rarr;](harden-redis-container-isolation.md): the same walkthrough for the other in-memory store.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
