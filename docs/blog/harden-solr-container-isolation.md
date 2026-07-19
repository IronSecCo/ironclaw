---
title: "How to harden a Solr container: solr:9 scores 63/100 by default"
description: "solr:9 defaults score 63/100 (grade C): full caps and a writable rootfs. The exact ironctl scan --fix flags that take it to 100/100 grade A."
---

# How to harden an Apache Solr container (and is solr:9 safe for untrusted workloads?)

Short answer: a stock `docker run solr:9` already runs as a non-root uid, but it is **not** a boundary
you should trust around untrusted code or an untrusted network. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **63 of 100, grade C (partial)**. Higher is
safer. Three runtime flags take the same image to **100 of 100, grade A**. This guide shows the exact
gaps and the exact fixes, straight from the scan data.

> Every number here comes from a read-only `docker inspect` of `solr:9`, the same data behind its
> [isolation scorecard](../scores/solr.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run solr:9`,
three of them fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as uid 8983 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Solr already ships the hardest win, a non-root uid. What is left is a full Linux capability set and a
writable root filesystem. A Solr instance that can reach the network is one that can exfiltrate every
indexed document the moment it is compromised (a malicious `stream.body` request, a data-import CVE),
and a writable rootfs is a persistence surface an attacker keeps after a restart.

## Harden it: the exact `--fix` remediation

`ironctl scan my-solr --fix` prints one remediation per failed dimension, then a single
copy-pasteable hardened run. For `solr:9`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Solr needs none of the defaults.
- **`--network=none`** (Network isolation, +11): if the search node is only reached by services on a
  private user-defined network, cut host egress entirely; otherwise attach a single internal network
  with no default route, not `bridge`.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/var/solr` as an explicit writable volume. Removes the persistence surface.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name solr -p 8983:8983 solr:9

# After: 100/100, grade A
docker run -d --name solr-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v solr-data:/var/solr \
  --network=none \
  solr:9
```

Rescan and the same seven dimensions all pass: `ironctl scan solr-hardened` reports
`100/100 grade A`. That is a **37-point swing from three one-line flags**, no image rebuild.

If Solr is queried by application servers over the network or runs as a SolrCloud cluster with peers
talking to ZooKeeper, keep the node hardened but hold the network dimension at a WARN with a scoped
internal network instead of `--network=none`. That is an honest **89/100, grade B** ceiling: the last
11 points pay for cutting the query connection your app needs.

## Verify it on your own search node

The grade above is the default image. Your deployment is what matters. Scan it in ten seconds:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-solr
ironctl scan my-solr --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Solr in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [solr:9 isolation scorecard &rarr;](../scores/solr.md): the full dimension breakdown.
- [Search engines, ranked by isolation &rarr;](../scores/collections/search-engines.md): how Solr compares to Elasticsearch, OpenSearch, Meilisearch, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
