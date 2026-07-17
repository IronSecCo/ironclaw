---
title: "How to harden a Valkey container: valkey:8 scores 48/100 by default"
description: "valkey:8 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a co-located cache to a full 100/100 grade A."
---

# How to harden a Valkey container (and is valkey:8 safe in your stack?)

Valkey is the community-driven Redis fork: the same in-memory key-value store used as a co-located
cache, session store, and rate limiter. Fast, and often holding session tokens and cached PII an
attacker would love to read. A stock `docker run valkey/valkey:8` keeps that store behind a boundary
weaker than the data deserves. Graded on IronClaw's seven-dimension containment scale, the default
configuration scores **48 of 100, grade D (porous)**. Higher is safer. When only its co-located
application talks to it over loopback, a cache can close every dimension, including the network. A few
runtime flags take the same image to a full **100 of 100, grade A**. Here are the exact gaps and fixes
from the scan data.

> Every number here comes from a read-only `docker inspect` of `valkey/valkey:8`, the same data behind
> its [isolation scorecard](../scores/valkey.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
valkey/valkey:8`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and **egress**. A Valkey process that escapes as root
escapes as root on the host. And a cache that can reach arbitrary destinations is one that can quietly
ship every cached session and secret out the moment a parsing or Lua-scripting CVE lands code
execution. The default capability set and writable rootfs widen and entrench that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-valkey --fix` prints one remediation per failed dimension, then one hardened run. For
`valkey/valkey:8`:

- **`--user 999:999`** (Non-root user, +15): pin the non-root `valkey` uid so an escape does not begin
  as host uid 0. Point `/data` at a volume this uid owns if you persist an RDB/AOF snapshot.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Valkey needs none of
  the default set to serve on its port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  the data directory as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  cache can max out. If the only client is an application on the same host or pod reaching Valkey over
  the loopback of a shared network namespace, cut the NIC entirely. Nothing external can connect, and
  the cache cannot phone home.

### When network=none is not honest

If Valkey is a shared cache that many services across the network connect to, or you run replication
to replicas, you cannot use `--network=none`; it has to accept those connections. In that case put it
on a user-defined network scoped to just its clients and replicas, with no default route out. That
holds the network dimension at a WARN (4 of 15) and the honest ceiling becomes **89 of 100, grade B**,
the same as a broker. Use `--network=none` only for the single-application, co-located case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name valkey valkey/valkey:8

# After: 100/100, grade A (co-located app on loopback, no network needed)
docker run -d --name valkey-hardened \
  --user 999:999 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v valkey-data:/data \
  --network=none \
  valkey/valkey:8
```

Rescan: `ironctl scan valkey-hardened` reports `100/100 grade A`. A **52-point swing** with no custom
image build, just the right flags. Every dimension is closed because a co-located cache does not need
to talk to anything but the app on the other side of its loopback. Run it as a shared network cache
instead and the honest ceiling is **89/100, grade B**, called out above.

## Verify it on your own Valkey

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-valkey
ironctl scan my-valkey --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Valkey in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [valkey isolation scorecard &rarr;](../scores/valkey.md): the full dimension breakdown.
- [How to harden a Redis container &rarr;](harden-redis-container-isolation.md): the store Valkey forked, with the same grade-A path.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
