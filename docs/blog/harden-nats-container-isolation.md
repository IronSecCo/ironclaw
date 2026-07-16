---
title: "How to harden a NATS container: nats:2.10-alpine scores 48/100 by default"
description: "nats:2.10-alpine defaults score 48/100 (grade D): full caps, writable rootfs. The exact ironctl scan --fix flags that take the messaging broker to its honest 89/100 grade B ceiling."
---

# How to harden a NATS container (and is nats:2.10-alpine safe for your messages?)

NATS is the nervous system of an event-driven stack: every service publishes to it and subscribes from
it, and with JetStream it also persists the stream. A stock `docker run nats:2.10-alpine` keeps that
broker behind a boundary weaker than the traffic deserves. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is safer.
A broker exists to be connected to, so it cannot take `--network=none` the way a co-located database
can. That sets an honest ceiling of **89 of 100, grade B**, and the flags below reach it. Here are the
exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `nats:2.10-alpine`, the same data behind
> its [isolation scorecard](../scores/nats.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run nats:2.10-alpine`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**. A NATS process that escapes as root escapes as root on
the host, right next to every message flowing through it and, with JetStream, the persisted stream on
disk. The default capability set and writable rootfs widen and entrench that foothold. The network
dimension stays a WARN by design here, because a broker has to accept connections.

## Harden it: the exact `--fix` remediation

`ironctl scan my-nats --fix` prints one remediation per failed dimension, then one hardened run. For
`nats:2.10-alpine`:

- **`--user 1000:1000`** (Non-root user, +15): pin a non-root uid so an escape does not begin as host
  uid 0. Point any JetStream store directory at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; NATS needs none of the
  default set to serve its client and monitoring ports.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  the JetStream data directory as an explicit writable volume. Removes the persistence surface.
- **Scoped private network** (Network, held at WARN by design): a broker exists to be connected to, so
  `--network=none` would break it. Put NATS on a user-defined network scoped to just its publishers,
  subscribers, and cluster peers, with no default route out. The network dimension holds at a WARN
  (4 of 15). That is the honest ceiling.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name nats nats:2.10-alpine

# After: 89/100, grade B (scoped private network for clients and peers)
docker run -d --name nats-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v nats-data:/data \
  --network=messaging-internal \
  nats:2.10-alpine
```

Rescan: `ironctl scan nats-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a broker exists to be reached by its publishers and subscribers; `network=none` would
score the last points but leave nothing able to connect. That is the honest ceiling for this role, and
it is a long way from the default D.

## Verify it on your own NATS

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-nats
ironctl scan my-nats --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade the
NATS in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [nats:2.10-alpine isolation scorecard &rarr;](../scores/nats.md): the full dimension breakdown.
- [How to harden a Kafka container &rarr;](harden-kafka-container-isolation.md): another broker whose honest ceiling is grade B.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
