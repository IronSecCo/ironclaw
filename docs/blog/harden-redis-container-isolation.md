---
title: "How to harden a Redis container: redis:7-alpine scores 48/100 by default"
description: "redis:7-alpine defaults score 48/100 (grade D): root, full caps, open egress. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden a Redis container (and is redis:7-alpine safe to expose?)

Short answer: the stock `docker run redis:7-alpine` is a weak boundary, and Redis has a long
history of turning a reachable instance into remote code execution (`CONFIG SET dir` plus a
malicious module or cron write). Graded on IronClaw's seven-dimension containment scale, the
default configuration scores **48 of 100, grade D (porous)**. Higher is safer. Six runtime
flags take the same image to **100 of 100, grade A**. Here are the exact gaps and fixes, from
the scan data.

> Every number here comes from a read-only `docker inspect` of `redis:7-alpine`, the same data
> behind its [isolation scorecard](../scores/redis.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run redis:7-alpine`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Redis makes the **writable rootfs** and **retained capabilities** especially sharp. The classic
unauthenticated-Redis exploit uses `CONFIG SET dir /` plus `dbfilename` to write an arbitrary
file (an SSH key, a cron job, a loadable module) to the host-visible filesystem. A read-only
rootfs and a dropped capability set take that primitive off the table before you even reach
authentication.

## Harden it: the exact `--fix` remediation

`ironctl scan my-redis --fix` prints one remediation per failed dimension, then assembles a
single hardened run. For `redis:7-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability. Redis needs
  none of the defaults for a normal keystore workload.
- **`--user 65532:65532`** (Non-root user, +15): run as a non-root uid so an escape does not
  land as host root. The official image already ships a `redis` user; a fixed non-root uid is
  the auditable choice.
- **`--network=none`** (Network isolation, +11): if only in-stack services talk to Redis, put
  it on an internal user-defined network with no egress instead of `bridge`. Also set a
  password (`--requirepass`) and rename the `CONFIG` command, but the isolation flags are the
  boundary that holds when auth does not.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): read-only root plus an explicit
  writable volume for persistence. This is the single highest-leverage flag against the
  file-write RCE.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name redis redis:7-alpine

# After: 100/100, grade A
docker run -d --name redis-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v redis-data:/data \
  --network=none \
  redis:7-alpine
```

Rescan: `ironctl scan redis-hardened` reports `100/100 grade A`. A **48-point swing from four
one-line flags**, no image rebuild.

## Verify it on your own instance

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-redis
ironctl scan my-redis --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the Redis in your real stack.

## Keep going

- [redis:7-alpine isolation scorecard &rarr;](../scores/redis.md): the full dimension breakdown.
- [Databases, ranked by isolation &rarr;](../scores/collections/databases.md): how Redis compares to Postgres, MySQL, Mongo, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
