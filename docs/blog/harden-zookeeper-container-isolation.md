---
title: "How to harden a ZooKeeper container: zookeeper:latest scores 48/100 by default"
description: "zookeeper:latest defaults score 48/100 (grade D): root execution, full caps, writable rootfs. The exact ironctl scan --fix flags that take an ensemble to its honest 89/100 grade B."
---

# How to harden a ZooKeeper container (and is zookeeper:latest safe for untrusted workloads?)

A distributed coordination service sits at the absolute core of your infrastructure state, which is exactly why its container isolation must be rock-solid. A stock `docker run zookeeper:latest` starts with significant risk out of the box, defaulting to root execution (`uid=0`), standard kernel capabilities, and an unconstrained root filesystem. Graded on IronClaw's seven-dimension containment scale, the default configuration scores **48 of 100, grade D (poor)**. Higher is safer. 

A handful of runtime flags takes the exact same image to **89 of 100, grade B**, two full letter grades higher. The single dimension it cannot max out is the one a coordination service requires by definition: client and quorum nodes must be able to communicate across the network. Here are the exact gaps, operational requirements, and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `zookeeper:latest` (`sha256:4c6f15fbd5491a3e01b0108c046891125553329a4956848ba3014cedff5386ee`), the same data behind its [isolation scorecard](../scores/zookeeper.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run zookeeper:latest`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | defaults to execution as root (uid=0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Unlike workloads that ship with non-root defaults, ZooKeeper inherits the baseline 48/100 score typical of root-by-default images. The primary vulnerabilities stem from **root execution**, **retained capabilities**, and a **writable root filesystem**. 

Securing ZooKeeper requires stripping privileges down to a non-root UID, dropping standard capabilities like `CAP_NET_RAW` to neutralize protocol-parsing exploits, and locking down the root filesystem to eliminate arbitrary persistent writes.

## Operational mechanics & bootstrap traps

Executing ZooKeeper under a strict `--read-only` constraint introduces specific runtime traps during initial bootstrap that will crash the container if not handled correctly:

1. **Dynamic Configuration:** The standard entrypoint script (`/docker-entrypoint.sh`) dynamically generates and writes `zoo.cfg` into `/conf` at boot time.
2. **State & Transaction Logs:** The Java process requires continuous write access to `/data` (snapshots) and `/datalog` (transaction logs), along with `/tmp` for transient execution files.

If these paths are not explicitly provided as writable memory buffers (`tmpfs`) with open permissions (`mode=1777`), initialization fails immediately with `EROFS` (*Read-only file system*) or `EACCES` (*Permission denied*) errors.

## Harden it: the exact `--fix` remediation

`ironctl scan my-zookeeper --fix` prints one remediation per failed dimension, followed by a hardened run specification. For `zookeeper:latest`:

- **`--user 1000:1000`** (Non-root user, +15): explicit user pinning forces the container away from `uid=0` root execution to an unprivileged account.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux kernel capability; ZooKeeper requires none of the defaults for standard operation.
- **`--read-only` + `tmpfs` mounts** (Read-only rootfs, +10): lock the root filesystem as read-only. Supply `--tmpfs /tmp:rw,mode=1777`, `--tmpfs /conf:rw,mode=1777`, `--tmpfs /data:rw,mode=1777`, and `--tmpfs /datalog:rw,mode=1777` to satisfy startup generation and transaction logging.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 points but breaks distributed consensus and client access. Any bridge or custom network scores 4 of 15 (a WARN, not a fail). Attaching a user-defined network scoped solely to cluster peers and clients prevents a compromised instance from establishing arbitrary egress.

## Before and after

```bash
# Before: 48/100, grade D (Default unhardened execution)
docker run -d --name zookeeper \
  zookeeper@sha256:4c6f15fbd5491a3e01b0108c046891125553329a4956848ba3014cedff5386ee

# After: 89/100, grade B (Hardened runtime specification)
docker run -d --name zookeeper-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only \
  --tmpfs /tmp:rw,mode=1777 \
  --tmpfs /conf:rw,mode=1777 \
  --tmpfs /data:rw,mode=1777 \
  --tmpfs /datalog:rw,mode=1777 \
  -p 2181:2181 \
  -p 2888:2888 \
  -p 3888:3888 \
  zookeeper@sha256:4c6f15fbd5491a3e01b0108c046891125553329a4956848ba3014cedff5386ee
```
## Verify it on your own Zookeeper

To audit and confirm the containment posture of a target workload, execute `ironctl scan` against the active container instance, Docker Compose service, or Kubernetes manifest:
```
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-zookeeper
ironctl scan my-zookeeper --fix
```
`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, allowing you to grade the ZooKeeper instance within your orchestration stack.

```
IronClaw containment scan
  target:  zookeeper-hardened
  runtime: crun
  score:   89/100  grade B  (solid, minor gaps)

DIMENSION                    VERDICT   SCORE  DETAIL
Non-root user (uid != 0)    [+] PASS  15/15  runs as 1000:1000 (uid != 0)
Dropped capabilities        [+] PASS  20/20  all capabilities dropped, none added back
Seccomp profile             [+] PASS  15/15  seccomp profile active (syscall surface filtered)
Network isolation / egress  [~] WARN   4/15  network=bridge: outbound egress is possible; prefer network=none
Read-only root filesystem   [+] PASS  10/10  root filesystem is read-only
No docker.sock exposure     [+] PASS  15/15  no docker.sock / OCI control socket mounted
No shared host namespaces   [+] PASS  10/10  no host PID/IPC/network namespace sharing
```

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [zookeeper isolation scorecard &rarr;](../scores/zookeeper.md): the full dimension breakdown.
- [How to harden a Consul container &rarr;](harden-consul-container-isolation.md): another coordination engine reaching grade B.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full ironctl scan reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary.
