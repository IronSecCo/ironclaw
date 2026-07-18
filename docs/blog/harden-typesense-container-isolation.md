---
title: "How to harden a Typesense container: typesense scores 48/100 by default"
description: "typesense defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a search server to its honest 89/100 grade B."
---

# How to harden a Typesense container (and is typesense safe in your stack?)

Typesense indexes your documents and answers every search query your app sends, so it holds a
searchable copy of the data behind your product and the API key that guards it. A stock
`docker run typesense/typesense` is not the boundary that role deserves. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **48 of 100, grade D (porous)**.
Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one a search server needs by definition: your app must
reach its API to run queries. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `typesense/typesense:27.1`, the same
> data behind its [isolation scorecard](../scores/typesense.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run typesense/typesense`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**. Typesense parses attacker-influenced input on every
request, the query strings and the documents you index; a parser CVE that lands code execution in a
root container escapes as root on the host, next to the index data and the admin API key. The full
capability set widens that foothold and the writable rootfs makes it durable.

## Harden it: the exact `--fix` remediation

`ironctl scan my-typesense --fix` prints one remediation per failed dimension, then one hardened run.
For `typesense`:

- **`--user 1000:1000`** (Non-root user, +15): run as a non-root uid so an escape does not begin as
  host uid 0. Point `--data-dir` at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Typesense serves its
  API on a high port and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  the data directory as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  search server, your app must reach it. Any named or bridge network scores 4 of 15 (a WARN, not a
  fail): a connection path exists. Contain it anyway: put Typesense on a user-defined network scoped
  to just the services that query it, with no default route out.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name typesense \
  -e TYPESENSE_API_KEY=xyz -e TYPESENSE_DATA_DIR=/data \
  typesense/typesense:27.1

# After: 89/100, grade B (scoped private network for its clients)
docker run -d --name typesense-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v typesense-data:/data \
  --network=app-internal \
  -e TYPESENSE_API_KEY=xyz -e TYPESENSE_DATA_DIR=/data \
  typesense/typesense:27.1
```

Rescan reports `89/100 grade B`, a **41-point swing** with no custom image build, just the right
flags. Proven directly on the hardened config with `ironctl scan --compose`:

```
score:   89/100  grade B  (solid, minor gaps)
Non-root user (uid != 0)    [+] PASS  15/15  runs as 1000:1000 (uid != 0)
Dropped capabilities        [+] PASS  20/20  all capabilities dropped, none added back
Read-only root filesystem   [+] PASS  10/10  root filesystem is read-only
Network isolation / egress  [~] WARN  4/15   network=bridge: outbound egress is possible
```

The only dimension still short of full marks is the network (4 of 15), because a search server exists
to be queried; `network=none` would score the last points but leave nothing able to reach it. That is
the honest ceiling for this role, and it is a long way from the default D.

## Verify it on your own Typesense

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-typesense
ironctl scan my-typesense --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Typesense in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [typesense isolation scorecard &rarr;](../scores/typesense.md): the full dimension breakdown.
- [How to harden a Meilisearch container &rarr;](harden-meilisearch-container-isolation.md): the other search engine, same ceiling.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
