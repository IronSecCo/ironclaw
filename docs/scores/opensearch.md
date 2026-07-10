---
title: "opensearch:2 container isolation score: 63/100 (grade C)"
description: "How isolated is opensearch:2 by default? IronClaw scores its sandbox posture 63/100 (C): retains default capabilities. Scan any container in 10s."
---

# opensearch:2 container isolation score: 63/100 (grade C)

Run with plain `docker run opensearchproject/opensearch:2` defaults, no hardening flags, the **opensearch** image scores **63/100, grade C (partial)** on IronClaw's seven-dimension container containment scale. Higher is safer. This is what you get straight out of a copy-pasted `docker run`; the fixes below close the gap.

> Graded from a read-only `docker inspect` of `opensearchproject/opensearch:2` at digest `sha256:b485da5f7f7bcb174523954bae1303c8c2ddaae9f1c2c89f10d625c98544e7cd`. No workload is executed. [How scoring works &rarr;](../scan.md)

## How it scores, dimension by dimension

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as 1000 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (includes CAP_NET_RAW, CAP_MKNOD, …) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active (syscall surface filtered) |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible; prefer network=none |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable: tamper/persistence surface |
| No docker.sock exposure | ✅ PASS | 15/15 | no docker.sock / OCI control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network namespace sharing |

## Harden it: the highest-value fixes

Applying these to your `docker run opensearch` closes the biggest gaps first (most points recovered first):

- **Dropped capabilities**, `--cap-drop=ALL`  
  Drop every Linux capability; add back only what the workload provably needs.
- **Network isolation / egress**, `--network=none`  
  Cut egress so a compromised workload cannot reach the network or exfiltrate.
- **Read-only root filesystem**, `--read-only --tmpfs /tmp`  
  Make the root filesystem read-only to remove the tamper/persistence surface.

A fully hardened run scores **100/100 (grade A)**:

```bash
docker run -d --name opensearch-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  opensearchproject/opensearch:2
```

## Scan your own container

These grades come from `ironctl scan`, a single, credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just this image:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own opensearch the same way this page was generated
ironctl scan my-opensearch
```

- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey this directory is built from.
- [Run untrusted code in a real sandbox &rarr;](../index.md), IronClaw wraps every AI-agent session in a gVisor/Kata isolation boundary with `network=none` by default.

---

*Part of the [Container Isolation Scores](index.md) directory, default-configuration containment grades for the most-pulled public images.*
