---
title: "How to harden an EMQX container: emqx:5 scores 63/100 by default"
description: "emqx:5 defaults score 63/100 (grade C): full caps and a writable rootfs. The exact ironctl scan --fix flags that take it to an honest 89/100 grade B ceiling."
---

# How to harden an EMQX container (and is emqx:5 safe for untrusted workloads?)

Short answer: a stock `docker run emqx:5` already runs as a non-root uid, but it is **not** a boundary
you should trust around untrusted code or an untrusted network. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **63 of 100, grade C (partial)**. Higher is safer.
A few runtime flags take the same image to an honest **89 of 100, grade B** ceiling, the most a broker
can reach without breaking the thing it exists to do: accept connections. This guide shows the exact
gaps, the exact fixes, and why 89 is the honest number.

> Every number here comes from a read-only `docker inspect` of `emqx:5`, the same data behind its
> [isolation scorecard](../scores/emqx.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run emqx:5`,
three of them fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as uid emqx (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

EMQX already ships the hardest win, a non-root uid. What is left is a full Linux capability set and a
writable root filesystem. An MQTT broker that keeps all its default capabilities is a broker where a
compromise (a malicious payload, a protocol CVE) starts with more Linux privilege than it will ever
need, and a writable rootfs is a persistence surface an attacker keeps across a restart.

## Harden it: the exact `--fix` remediation

`ironctl scan my-emqx --fix` prints one remediation per failed dimension, then a single
copy-pasteable hardened run. For `emqx:5`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. EMQX needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/opt/emqx/data` as an explicit writable volume. Removes the persistence surface.
- **Scope the network, do not cut it** (Network isolation, held at WARN): a broker exists to be
  connected to, so `--network=none` would break it. Put EMQX on a dedicated internal Docker network
  that only its publishers and subscribers join, with no default route to the host or internet.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name emqx -p 1883:1883 emqx:5

# After: 89/100, grade B (the honest broker ceiling)
docker network create --internal mqtt-net
docker run -d --name emqx-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v emqx-data:/opt/emqx/data \
  --network=mqtt-net \
  emqx:5
```

Rescan and six of seven dimensions pass; the network dimension holds at a WARN by design:
`ironctl scan emqx-hardened` reports `89/100 grade B`. That is a **26-point swing from a few
one-line flags**, no image rebuild.

## Why 89 is the ceiling, not a failure

The last 11 points live in the network dimension, and a broker cannot claim them without ceasing to
be a broker. Publishers and subscribers have to reach the listener, so the honest posture is a
**scoped** network, not **no** network. We hold the dimension at a WARN and say so, rather than mount
a fake `--network=none` that would score 100 and drop every MQTT client. The mitigation that matters
is blast radius: an internal network with no default route means a compromised EMQX can talk to its
clients and nothing else, not the host, not the internet.

## Verify it on your own broker

The grade above is the default image. Your deployment is what matters. Scan it in ten seconds:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-emqx
ironctl scan my-emqx --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the EMQX in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [emqx:5 isolation scorecard &rarr;](../scores/emqx.md): the full dimension breakdown.
- [Message queues, ranked by isolation &rarr;](../scores/collections/message-queues.md): how EMQX compares to Kafka, RabbitMQ, NATS, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
