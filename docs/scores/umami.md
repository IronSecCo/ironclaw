---
title: "umami:postgresql-v2.14.0 container isolation score: 63/100 (grade C)"
description: "How isolated is umami:postgresql-v2.14.0 by default? IronClaw scores its sandbox posture 63/100 (C): retains default capabilities. Scan any container in 10s."
---

# umami:postgresql-v2.14.0 container isolation score: 63/100 (grade C)

Run with plain `docker run ghcr.io/umami-software/umami:postgresql-v2.14.0` defaults, no hardening flags, the **umami** image scores **63/100, grade C (partial)** on IronClaw's seven-dimension container containment scale. Higher is safer. This is what you get straight out of a copy-pasted `docker run`; the fixes below close the gap.

> Graded from a read-only `docker inspect` of `ghcr.io/umami-software/umami:postgresql-v2.14.0` at digest `sha256:54ccf9903edf09791889504360e776b52c358ebddf5a5e8662257b2555608f27`. No workload is executed. [How scoring works &rarr;](../scan.md)

## How it scores, dimension by dimension

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as nextjs (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (includes CAP_NET_RAW, CAP_MKNOD, …) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active (syscall surface filtered) |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible; prefer network=none |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable: tamper/persistence surface |
| No docker.sock exposure | ✅ PASS | 15/15 | no docker.sock / OCI control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network namespace sharing |

## Harden it: the highest-value fixes

Applying these to your `docker run umami` closes the biggest gaps first (most points recovered first):

- **Dropped capabilities**, `--cap-drop=ALL`  
  Drop every Linux capability; add back only what the workload provably needs.
- **Network isolation / egress**, `--network=none`  
  Cut egress so a compromised workload cannot reach the network or exfiltrate.
- **Read-only root filesystem**, `--read-only --tmpfs /tmp`  
  Make the root filesystem read-only to remove the tamper/persistence surface.

A fully hardened run scores **100/100 (grade A)**:

```bash
docker run -d --name umami-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  ghcr.io/umami-software/umami:postgresql-v2.14.0
```

## Scan your own container

These grades come from `ironctl scan`, a single, credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just this image:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own umami the same way this page was generated
ironctl scan my-umami
```

- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey this directory is built from.
- [Run untrusted code in a real sandbox &rarr;](../index.md), IronClaw wraps every AI-agent session in a gVisor/Kata isolation boundary with `network=none` by default.

## Badge this image

Maintain **umami** (or run it)? Show its default-config isolation score with a badge that links back to this scorecard:

[![Container Isolation Score: 63/100 C](https://img.shields.io/badge/container%20isolation-63%2F100%20C-d4a72c)](https://ironsecco.github.io/ironclaw/scores/umami/)

```markdown
[![Container Isolation Score: 63/100 C](https://img.shields.io/badge/container%20isolation-63%2F100%20C-d4a72c)](https://ironsecco.github.io/ironclaw/scores/umami/)
```

The badge is a plain [shields.io](https://shields.io) URL: no server, no build step, nothing to host. It reflects this page's default-configuration grade. Hardened your own deployment? Generate a live badge of *your* config with [`ironctl scan --badge-json`](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md), or compare every image on the [leaderboard](leaderboard.md).

---

*Part of the [Container Isolation Scores](index.md) directory, default-configuration containment grades for the most-pulled public images.*
