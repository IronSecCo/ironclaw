---
title: "How to harden a Neo4j container: neo4j:5 scores 48/100 by default"
description: "neo4j:5 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a co-located graph database to a full 100/100 grade A."
---

# How to harden a Neo4j container (and is neo4j:5 safe for your graph?)

Neo4j holds a connected picture of your data: identity graphs, fraud rings, recommendation edges, and
the relationships an attacker would love to walk. A stock `docker run neo4j:5` keeps that graph behind
a boundary weaker than the data deserves. Graded on IronClaw's seven-dimension containment scale, the
default configuration scores **48 of 100, grade D (porous)**. Higher is safer. Unlike a broker or a
proxy, a graph database that only its co-located application talks to can close every dimension,
including the network. A few runtime flags take the same image to a full **100 of 100, grade A**. Here
are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `neo4j:5`, the same data behind its
> [isolation scorecard](../scores/neo4j.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run neo4j:5`,
three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and **egress**. A Neo4j process that escapes as root
escapes as root on the host, next to the very store files it was holding. And a database that can
reach arbitrary destinations is one that can quietly ship your entire graph out the moment a Cypher
parsing or plugin CVE lands code execution. The default capability set and writable rootfs widen and
entrench that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-neo4j --fix` prints one remediation per failed dimension, then one hardened run. For
`neo4j:5`:

- **`--user 7474:7474`** (Non-root user, +15): pin the non-root `neo4j` uid so an escape does not
  begin as host uid 0. Point the data and logs directories at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Neo4j needs none of
  the default set to serve Bolt and HTTP on high ports.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/data` and `/logs` as explicit writable volumes. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  store can actually max out. If the only client is an application on the same host or pod reaching
  Neo4j over the loopback of a shared network namespace, cut the NIC entirely. Nothing external can
  connect, and the database cannot phone home.

### When network=none is not honest

If remote clients or Neo4j browser users connect over the network, or you run a causal cluster with
peer connections between core members, you cannot use `--network=none`; the store has to accept those
connections. In that case put it on a user-defined network scoped to just its clients and cluster
peers, with no default route out. That holds the network dimension at a WARN (4 of 15) and the honest
ceiling becomes **89 of 100, grade B**, the same as a broker. Use `--network=none` only for the
single-application, co-located case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name neo4j neo4j:5

# After: 100/100, grade A (co-located store, no network needed)
docker run -d --name neo4j-hardened \
  --user 7474:7474 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v neo4j-data:/data -v neo4j-logs:/logs \
  --network=none \
  neo4j:5
```

Rescan: `ironctl scan neo4j-hardened` reports `100/100 grade A`. A **52-point swing** with no custom
image build, just the right flags. Every dimension is closed because a co-located graph database does
not need to talk to anything but the app on the other side of its loopback. That is the top grade,
reserved for datastores whose clients live next to them.

## Verify it on your own Neo4j

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-neo4j
ironctl scan my-neo4j --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Neo4j in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [neo4j:5 isolation scorecard &rarr;](../scores/neo4j.md): the full dimension breakdown.
- [How to harden a MongoDB container &rarr;](harden-mongodb-container-isolation.md): another document-shaped store that reaches grade A when co-located.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
