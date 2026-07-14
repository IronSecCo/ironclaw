---
title: "How to harden a Kafka container: apache/kafka:3.9.0 scores 63/100 by default"
description: "apache/kafka:3.9.0 defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take a broker to its honest 89/100 grade B."
---

# How to harden a Kafka container (and is apache/kafka:3.9.0 safe for untrusted workloads?)

A broker sits between every service you run, which is exactly why its container should be one of
the tightest. A stock `docker run apache/kafka:3.9.0` is closer than most, it already runs as a
non-root user, but graded on IronClaw's seven-dimension containment scale it still scores only
**63 of 100, grade C (partial)**. Higher is safer. A couple of runtime flags take the same image to
**89 of 100, grade B**, one point off an A, and the one dimension it cannot reach is the one a
broker needs by definition (clients must connect to it). Here are the exact gaps and fixes from the
scan data.

> Every number here comes from a read-only `docker inspect` of `apache/kafka:3.9.0`, the same data
> behind its [isolation scorecard](../scores/kafka.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
apache/kafka:3.9.0`, three fail or warn (and, notably, non-root already passes):

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as appuser (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Apache's image already did the hardest part, it drops root. The sharpest edges that remain are the
**retained capabilities** and the **writable rootfs**: a plugin or connector CVE that lands code
execution lands it with `CAP_NET_RAW` and friends, on a process every service in your stack already
connects to, and with a filesystem it can rewrite to persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-kafka --fix` prints one remediation per failed dimension, then one hardened run.
For `apache/kafka:3.9.0`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Kafka needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/var/lib/kafka` as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  broker, clients must be able to connect to it. Any named or bridge network scores 4 of 15 (a
  WARN, not a fail): a connection path exists. This is the one dimension a broker cannot max out.
  Contain it anyway: attach a user-defined network scoped to just its producers and consumers, with
  no default route out, so a compromised broker cannot call arbitrary internet addresses.

No `--user` flag is needed: the image already runs as `appuser`.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name kafka apache/kafka:3.9.0

# After: 89/100, grade B (scoped private network for producers and consumers)
docker run -d --name kafka-hardened \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v kafka-data:/var/lib/kafka \
  --network=kafka-internal \
  apache/kafka:3.9.0
```

Rescan: `ironctl scan kafka-hardened` reports `89/100 grade B`. A **26-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a broker exists to be connected to; `network=none` would score the last points but
leave nothing able to reach the topics. That is the honest ceiling for a broker, and it is a clear
step up from the default C.

## Verify it on your own broker

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-kafka
ironctl scan my-kafka --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Kafka in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [kafka:3.9.0 isolation scorecard &rarr;](../scores/kafka.md): the full dimension breakdown.
- [Message queues, ranked by isolation &rarr;](../scores/collections/message-queues.md): how Kafka compares to RabbitMQ, NATS, Redpanda, and the rest.
- [How to harden a RabbitMQ container &rarr;](harden-rabbitmq-container-isolation.md): another broker with the same honest network ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
