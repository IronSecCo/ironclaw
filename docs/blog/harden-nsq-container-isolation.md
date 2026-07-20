---
title: "How to harden an NSQ container: nsqio/nsq:v1.3.0 scores 48/100 by default"
description: "nsqio/nsq:v1.3.0 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a message broker to its honest 89/100 grade B."
---

# How to harden an NSQ container (and is nsqio/nsq:v1.3.0 safe for untrusted workloads?)

NSQ is a realtime distributed message queue that every producer and consumer in your system connects
to, which is exactly why its container should be one of the tightest. A stock `docker run
nsqio/nsq:v1.3.0` is not: graded on IronClaw's seven-dimension containment scale it scores **48 of
100, grade D (porous)**. Higher is safer. A short list of runtime flags takes the same image to **89
of 100, grade B**, one point off an A, and the one dimension it cannot reach is the one a broker needs
by definition (publishers and subscribers must connect to it). Here are the exact gaps and fixes from
the scan data.

> Every number here comes from a read-only `docker inspect` of `nsqio/nsq:v1.3.0`, the same data
> behind its [isolation scorecard](../scores/nsq.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
nsqio/nsq:v1.3.0`, four fail or warn:

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
protocol or client CVE that lands code execution lands it as uid 0, with `CAP_NET_RAW` and friends,
on the process every message in your stack passes through, and with a filesystem it can rewrite to
persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-nsq --fix` prints one remediation per failed dimension, then one hardened run.
For `nsqio/nsq:v1.3.0`:

- **`--user 65532:65532`** (Non-root, +15): run `nsqd` as an unprivileged uid. Point the data
  directory (`--data-path`) at a volume that uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. NSQ needs none of the defaults for a standard listen on its TCP and
  HTTP ports.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount the queue data path as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  broker, publishers and subscribers must reach it. Any named or bridge network scores 4 of 15 (a
  WARN, not a fail): a connection path exists. This is the one dimension a broker cannot max out.
  Contain it anyway: attach a user-defined network scoped to just its clients, with no default route
  out, so a compromised broker cannot call arbitrary internet addresses.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name nsq nsqio/nsq:v1.3.0 /nsqd

# After: 89/100, grade B (scoped private network for publishers and subscribers)
docker run -d --name nsq-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v nsq-data:/data \
  --network=nsq-internal \
  nsqio/nsq:v1.3.0 /nsqd --data-path=/data
```

Rescan: `ironctl scan nsq-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a broker exists to be connected through; `network=none` would score the last points but
leave nothing able to publish or subscribe. That is the honest ceiling for a broker, and it is a
clear step up from the default D.

## Verify it on your own broker

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-nsq
ironctl scan my-nsq --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the NSQ in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [nsq:v1.3.0 isolation scorecard &rarr;](../scores/nsq.md): the full dimension breakdown.
- [Message queues, ranked by isolation &rarr;](../scores/collections/message-queues.md): how NSQ compares to Kafka, RabbitMQ, NATS, and the rest.
- [How to harden a NATS container &rarr;](harden-nats-container-isolation.md): another lightweight broker with the same honest network ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
