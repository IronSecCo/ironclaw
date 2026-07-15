---
title: "How to harden a Consul container: hashicorp/consul:1.20 scores 48/100 by default"
description: "hashicorp/consul:1.20 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a service-mesh agent to its honest 89/100 grade B."
---

# How to harden a Consul container (and is hashicorp/consul:1.20 safe for untrusted workloads?)

Consul is the address book for your whole cluster: it knows where every service lives, holds their
health state, and often gates their access. That makes it a high-value target, and a stock
`docker run hashicorp/consul:1.20` is not the boundary that role deserves. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **48 of 100, grade D (porous)**.
Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one a service-discovery agent needs by definition
(its clients and peers must connect to it). Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `hashicorp/consul:1.20`, the same data
> behind its [isolation scorecard](../scores/consul.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
hashicorp/consul:1.20`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For a service-mesh control agent, the two that should worry you most are **root** and **egress**. A
Consul process that escapes as root escapes as root on the host, next to the ACL tokens and service
registry it was just holding. And a Consul process that can reach arbitrary destinations is one that
can quietly ship its catalog and secrets out the moment an RPC or plugin CVE lands code execution.

## Harden it: the exact `--fix` remediation

`ironctl scan my-consul --fix` prints one remediation per failed dimension, then one hardened run.
For `hashicorp/consul:1.20`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. See the mlock note below.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the data directory at a volume this uid owns.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/consul/data` as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  service-discovery agent, its peers and clients must reach it. Any named or bridge network scores 4
  of 15 (a WARN, not a fail): a connection path exists. This is the one dimension a mesh agent cannot
  max out. Contain it anyway: attach a user-defined network scoped to just the cluster members and
  the apps that register with them, with no default route out, so a compromised Consul cannot call
  arbitrary internet addresses.

### The mlock note

Consul can be told to `mlock` its memory so its data is never swapped to disk (`disable_mlock =
false`), and `mlock` needs the `IPC_LOCK` capability. Unlike Vault, Consul leaves mlock **disabled by
default**, so `--cap-drop=ALL` with nothing added back is the normal posture. If you deliberately
enable mlock, add just that one capability back with `--cap-add=IPC_LOCK`, or make sure swap is off
on the host so nothing hits disk anyway. The score above reflects dropping the default set.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name consul hashicorp/consul:1.20

# After: 89/100, grade B (scoped private network for its peers and clients)
docker run -d --name consul-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v consul-data:/consul/data \
  --network=consul-internal \
  hashicorp/consul:1.20
```

Rescan: `ironctl scan consul-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a service-discovery agent exists to be connected to; `network=none` would score the
last points but leave nothing able to register or resolve. That is the honest ceiling for a mesh
agent, and it is a long way from the default D.

## Verify it on your own Consul

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-consul
ironctl scan my-consul --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Consul in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [consul:1.20 isolation scorecard &rarr;](../scores/consul.md): the full dimension breakdown.
- [How to harden a Vault container &rarr;](harden-vault-container-isolation.md): the other HashiCorp service, with the same honest ceiling and the mlock story it borrows from.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
