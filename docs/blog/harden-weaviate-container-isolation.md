---
title: "How to harden a Weaviate container: weaviate:1.28.1 scores 48/100 by default"
description: "semitechnologies/weaviate:1.28.1 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a vector database to 100/100 grade A."
---

# How to harden a Weaviate container (and is weaviate:1.28.1 safe for untrusted workloads?)

A vector database backs your retrieval and RAG pipelines, so it sees a lot of untrusted input and
its container should be tight. A stock `docker run semitechnologies/weaviate:1.28.1` is not: graded
on IronClaw's seven-dimension containment scale it scores **48 of 100, grade D (porous)**. Higher is
safer. A short list of runtime flags takes the same image to **100 of 100, grade A**, because a
vector store that only its co-located app queries can drop its network entirely. Here are the exact
gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of
> `semitechnologies/weaviate:1.28.1`, the same data behind its [isolation
> scorecard](../scores/weaviate.md). No workload is executed. [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
semitechnologies/weaviate:1.28.1`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The sharpest edges are **root**, the **retained capabilities**, and the **writable rootfs**: a module
or import-path CVE that lands code execution lands it as uid 0, with `CAP_NET_RAW` and friends, on a
filesystem it can rewrite to persist its embeddings-poisoning payload.

## Harden it: the exact `--fix` remediation

`ironctl scan my-weaviate --fix` prints one remediation per failed dimension, then one hardened run.
For `semitechnologies/weaviate:1.28.1`:

- **`--user 1000:1000`** (Non-root, +15): run the process as an unprivileged uid. Point the
  persistence path at a volume that uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Weaviate needs none of the defaults.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount the data path as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11): a Weaviate that only its co-located app queries
  needs no external network at all. Attach it to the app over a private compose network and cut the
  default route out. This is the dimension that takes it to a full A.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name weaviate semitechnologies/weaviate:1.28.1

# After: 100/100, grade A
docker run -d --name weaviate-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v weaviate-data:/var/lib/weaviate \
  --network=none \
  semitechnologies/weaviate:1.28.1
```

Rescan: `ironctl scan weaviate-hardened` reports `100/100 grade A`. A **52-point swing** with no
custom image build, just the right flags.

> **Serving remote clients?** If apps outside the compose network hit Weaviate's REST or gRPC API,
> `--network=none` is wrong. Keep the other four fixes and attach a scoped private network; the honest
> ceiling is then **89/100, grade B**, with the network dimension held at a WARN. A single-node store
> with a co-located app reaches the full A.

## Verify it on your own database

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-weaviate
ironctl scan my-weaviate --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Weaviate in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [weaviate:1.28.1 isolation scorecard &rarr;](../scores/weaviate.md): the full dimension breakdown.
- [Vector databases, ranked by isolation &rarr;](../scores/collections/vector-databases.md): how Weaviate compares to Qdrant, Milvus, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
