---
title: "How to harden a RabbitMQ container: rabbitmq:4-alpine scores 48/100 by default"
description: "rabbitmq:4-alpine defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a broker to its honest 89/100 grade B."
---

# How to harden a RabbitMQ container (and is rabbitmq:4-alpine safe for untrusted workloads?)

A message broker sits between every service you run, which is exactly why its container should
be one of the tightest. A stock `docker run rabbitmq:4-alpine` is not that. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **48 of 100, grade D
(porous)**. Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**,
one point off an A, and the one dimension it cannot reach is the one a broker needs by
definition (clients must connect to it). Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `rabbitmq:4-alpine`, the same
> data behind its [isolation scorecard](../scores/rabbitmq.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run rabbitmq:4-alpine`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For a broker, the sharpest edges are the **root process** and **retained capabilities**: a
plugin or management-API CVE that lands code execution lands it as root with `CAP_NET_RAW` and
friends, on a process every service in your stack already trusts and connects to.

## Harden it: the exact `--fix` remediation

`ironctl scan my-rabbitmq --fix` prints one remediation per failed dimension, then one hardened
run. For `rabbitmq:4-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only
  what the workload provably needs. RabbitMQ needs none of the defaults.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin
  as host uid 0. Point the data directory at a volume this uid owns.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  broker, clients must be able to connect to it. Any named or bridge network scores 4 of 15 (a
  WARN, not a fail): a connection path exists. This is the one dimension a broker cannot max out.
  Contain it anyway: attach a user-defined network scoped to just its producers and consumers,
  with no default route out, so a compromised broker cannot call arbitrary internet addresses.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/var/lib/rabbitmq` as an explicit writable volume. Removes the persistence surface.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name rabbitmq rabbitmq:4-alpine

# After: 89/100, grade B (scoped private network for producers and consumers)
docker run -d --name rabbitmq-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v rabbitmq-data:/var/lib/rabbitmq \
  --network=rabbit-internal \
  rabbitmq:4-alpine
```

Rescan: `ironctl scan rabbitmq-hardened` reports `89/100 grade B`. A **41-point swing** with no
custom image build, just the right flags. The only dimension still short of full marks is the
network (4 of 15), because a broker exists to be connected to; `network=none` would score the
last points but leave nothing able to reach the queue. That is the honest ceiling for a broker,
and it is a long way from the default D.

## Verify it on your own broker

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-rabbitmq
ironctl scan my-rabbitmq --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the RabbitMQ in your stack, not just a bare `docker run`.

## Keep going

- [rabbitmq:4-alpine isolation scorecard &rarr;](../scores/rabbitmq.md): the full dimension breakdown.
- [Message queues, ranked by isolation &rarr;](../scores/collections/message-queues.md): how RabbitMQ compares to Kafka, NATS, Redpanda, and the rest.
- [How to harden an nginx container &rarr;](harden-nginx-container-isolation.md): another service with an honest network ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
