---
title: "How to harden a ZooKeeper container: zookeeper scores 55/100 by default"
description: "zookeeper:latest defaults score 55/100 (grade C): full capabilities, writable rootfs. The exact ironctl scan flags that take a distributed coordinator to its honest ceiling of 89/100 grade B."
---

# How to harden a ZooKeeper container (and is zookeeper:latest safe for your coordination data?)

Apache ZooKeeper is a centralized coordination service for distributed applications, managing configuration information, naming, distributed synchronization, and group services for Kafka, Hadoop, and Solr clusters. A stock `docker run zookeeper:latest` places that critical coordination backbone behind a boundary weaker than your cluster state deserves. Graded on IronClaw's seven-dimension containment scale, the default configuration scores **55 of 100, grade C (weak)**. Higher is safer.

Because a ZooKeeper instance inherently exists to communicate with ensemble peers and client applications across the network, its network isolation dimension remains open. However, applying a few runtime flags eliminates unnecessary kernel capabilities and filesystem attack surface, raising the container to its honest ceiling of **89 of 100, grade B**. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `zookeeper:latest`, the same data behind its [isolation scorecard](../scores/zookeeper.md). No workload is executed. [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run zookeeper:latest`, two fail and one is flagged:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as zookeeper (uid 1000); non-root user active |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 0/15 | network=bridge: outbound egress and peer communication active |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

While ZooKeeper runs as non-root (`zookeeper`), two major security gaps remain. Retaining default Linux capabilities grants privileges like `CAP_NET_RAW` and `CAP_MKNOD`, enabling potential raw socket creation or device node manipulation if compromised. Simultaneously, a writable root filesystem allows an attacker exploiting a coordination flaw to write persistent execution scripts or drop malicious binaries directly to disk.

## Harden it: the exact `--fix` remediation

`ironctl scan my-zookeeper --fix` prints one remediation per failed dimension, followed by a hardened run command. For `zookeeper:latest`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability. ZooKeeper does not require elevated kernel privileges to synchronize data or bind to client/quorum ports.
- **`--read-only --tmpfs /tmp --tmpfs /datalog`** (Read-only rootfs, +10): lock the root filesystem as read-only. Provide volatile `tmpfs` mounts for temporary files and transaction logs while persisting snapshot state to an explicit volume.
- **`--security-opt=no-new-privileges`**: prevent processes inside the container from gaining additional privileges via setuid binaries.

### Why Grade B is the honest ceiling

Unlike a co-located database that can run with `--network=none`, a coordination engine exists to manage quorum state (ports 2888/3888) and serve client requests (port 2181). Completely severing network access would prevent ZooKeeper from forming an ensemble or coordinating services. Because network communication remains active, the network dimension stays at a WARN (4 of 15), capping the maximum achievable score at **89 of 100, grade B**. Stating anything higher would be dishonest for a distributed coordinator.

## Before and after

```bash
# Before: 55/100, grade C
docker run -d --name zookeeper -p 2181:2181 zookeeper:latest

# After: 89/100, grade B (honest ceiling for coordination engines)
docker run -d --name zookeeper-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid \
  --tmpfs /datalog:rw,noexec,nosuid \
  -v zookeeper-data:/data \
  -p 2181:2181 \
  -p 2888:2888 \
  -p 3888:3888 \
  zookeeper:latest
```
Rescan: `ironctl scan zookeeper-hardened` reports `89/100 grade B`. A 34-point swing achieved purely through runtime flags without building a custom container image.

## Verify it on your own ZooKeeper

```
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-zookeeper
ironctl scan my-zookeeper --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, allowing you to grade ZooKeeper deployments directly in your pipeline.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [zookeeper isolation scorecard &rarr;](../scores/zookeeper.md): the full dimension breakdown.
- [How to harden a Consul container &rarr;](harden-consul-container-isolation.md): another coordination engine reaching grade B.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full ironctl scan reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary.
