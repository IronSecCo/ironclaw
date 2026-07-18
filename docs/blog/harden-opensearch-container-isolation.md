---
title: "How to harden an OpenSearch container: opensearch:2 scores 63/100 by default"
description: "opensearch:2 defaults score 63/100 (grade C): full caps, writable rootfs, open egress. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden an OpenSearch container (and is opensearch:2 safe for untrusted workloads?)

Short answer: a stock `docker run opensearchproject/opensearch:2` starts ahead of most images (it
runs non-root out of the box) but is still **not** a boundary to trust around an untrusted network.
Graded on IronClaw's seven-dimension containment scale, the default configuration scores
**63 of 100, grade C (partial)**. Higher is safer. Three runtime flags take the same image to
**100 of 100, grade A**. This guide shows the exact gaps and the exact fixes, straight from the
scan data.

> Every number here comes from a read-only `docker inspect` of `opensearchproject/opensearch:2`,
> the same data behind its [isolation scorecard](../scores/opensearch.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run opensearchproject/opensearch:2`, three fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as 1000 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

OpenSearch already runs as a non-root uid, which is why it clears the default 48/100 that
databases like MySQL and Postgres sit at. The remaining leaks are the **retained capabilities**
and **open egress**. An OpenSearch that can reach the network is one that can exfiltrate every
document it indexes the moment a query DSL or plugin CVE lands code execution, and the full
capability set hands that code `CAP_NET_RAW` and friends to work with.

## Harden it: the exact `--fix` remediation

`ironctl scan my-opensearch --fix` prints one remediation per failed dimension, then one hardened
run. For `opensearchproject/opensearch:2`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only
  what the workload provably needs. OpenSearch needs none of the defaults.
- **`--network=none`** (Network isolation, +11): for a single-node index reached only by
  co-located services on a private user-defined network, cut host egress entirely; otherwise
  attach a single internal network with no default route, not `bridge`. (A multi-node cluster
  needs inter-node transport on a scoped private network, which is a WARN, not a clean pass,
  see the ceiling note below.)
- **`--user 65532:65532`** (Non-root user, already passing at +0): non-root already passes at
  uid 1000. Pinning an explicit fixed uid is the auditable choice and keeps volume ownership
  unambiguous; it does not change the score.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/usr/share/opensearch/data` as an explicit writable volume. Removes the persistence
  surface.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name opensearch \
  -e discovery.type=single-node \
  opensearchproject/opensearch:2

# After: 100/100, grade A (single-node)
docker run -d --name opensearch-hardened \
  -e discovery.type=single-node \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v opensearch-data:/usr/share/opensearch/data \
  --network=none \
  opensearchproject/opensearch:2
```

Rescan and the same seven dimensions all pass: `ironctl scan opensearch-hardened` reports
`100/100 grade A`. That is a **37-point swing from three one-line flags**, no image rebuild.

## The honest ceiling for a multi-node cluster

The 100/100 above is a **single-node** index with `network=none`. A multi-node OpenSearch cluster
cannot use `network=none`: nodes must reach each other over the transport port. Attach a private
user-defined network scoped to just the cluster with no default route out, and the network
dimension scores 4 of 15 (a WARN, not a fail) instead of the full 15. That puts a hardened
multi-node cluster at **89 of 100, grade B**, one point off an A. The other six dimensions still
max out. That is the honest ceiling when nodes must talk, and it is a long way from the default C.

## Verify it on your own cluster

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-opensearch
ironctl scan my-opensearch --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the OpenSearch in your stack, not just a bare `docker run`.

## Keep going

- [opensearch:2 isolation scorecard &rarr;](../scores/opensearch.md): the full dimension breakdown.
- [Search engines, ranked by isolation &rarr;](../scores/collections/search-engines.md): how OpenSearch compares to Elasticsearch, Solr, Meilisearch, and the rest.
- [How to harden an Elasticsearch container &rarr;](harden-elasticsearch-container-isolation.md): the same walkthrough for the upstream OpenSearch forked from.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
