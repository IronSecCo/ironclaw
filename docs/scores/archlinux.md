---
title: "archlinux:latest container isolation score: 48/100 (grade D)"
description: "How isolated is archlinux:latest by default? IronClaw scores its sandbox posture 48/100 (D): retains default capabilities. Scan any container in 10s."
---

# archlinux:latest container isolation score: 48/100 (grade D)

Run with plain `docker run archlinux:latest` defaults, no hardening flags, the **archlinux** image scores **48/100, grade D (porous)** on IronClaw's seven-dimension container containment scale. Higher is safer. This is what you get straight out of a copy-pasted `docker run`; the fixes below close the gap.

> Graded from a read-only `docker inspect` of `archlinux:latest` at digest `sha256:fe6972d4dc1f660c0c10f4c41b2de8986bab89e7e2955378f8beadb8ebcd7433`. No workload is executed. [How scoring works &rarr;](../scan.md)

## How it scores, dimension by dimension

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (user "0 (default)"); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (includes CAP_NET_RAW, CAP_MKNOD, …) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active (syscall surface filtered) |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible; prefer network=none |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable: tamper/persistence surface |
| No docker.sock exposure | ✅ PASS | 15/15 | no docker.sock / OCI control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network namespace sharing |

## Harden it: the highest-value fixes

Applying these to your `docker run archlinux` closes the biggest gaps first (most points recovered first):

- **Dropped capabilities**, `--cap-drop=ALL`  
  Drop every Linux capability; add back only what the workload provably needs.
- **Non-root user (uid != 0)**, `--user 65532:65532`  
  Pin a non-root uid so a container escape does not begin as host uid 0.
- **Network isolation / egress**, `--network=none`  
  Cut egress so a compromised workload cannot reach the network or exfiltrate.
- **Read-only root filesystem**, `--read-only --tmpfs /tmp`  
  Make the root filesystem read-only to remove the tamper/persistence surface.

A fully hardened run scores **100/100 (grade A)**:

```bash
docker run -d --name archlinux-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  archlinux:latest
```

## Scan your own container

These grades come from `ironctl scan`, a single, credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just this image:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own archlinux the same way this page was generated
ironctl scan my-archlinux
```

- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey this directory is built from.
- [Run untrusted code in a real sandbox &rarr;](../index.md), IronClaw wraps every AI-agent session in a gVisor/Kata isolation boundary with `network=none` by default.

## Badge this image

Maintain **archlinux** (or run it)? Show its default-config isolation score with a badge that links back to this scorecard:

[![Container Isolation Score: 48/100 D](https://img.shields.io/badge/container%20isolation-48%2F100%20D-e8873a)](https://ironsecco.github.io/ironclaw/scores/archlinux/)

```markdown
[![Container Isolation Score: 48/100 D](https://img.shields.io/badge/container%20isolation-48%2F100%20D-e8873a)](https://ironsecco.github.io/ironclaw/scores/archlinux/)
```

The badge is a plain [shields.io](https://shields.io) URL: no server, no build step, nothing to host. It reflects this page's default-configuration grade. Hardened your own deployment? Generate a live badge of *your* config with [`ironctl scan --badge-json`](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md), or compare every image on the [leaderboard](leaderboard.md).

---

*Part of the [Container Isolation Scores](index.md) directory, default-configuration containment grades for the most-pulled public images.*
