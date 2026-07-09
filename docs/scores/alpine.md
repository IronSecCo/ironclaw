---
title: "alpine:3.21 container isolation score: 48/100 (grade D)"
description: "How isolated is alpine:3.21 by default? IronClaw scores its sandbox posture 48/100 (D): retains default capabilities, runs as root. Scan any container in 10s."
---

# alpine:3.21 container isolation score: 48/100 (grade D)

Run with plain `docker run alpine:3.21` defaults, no hardening flags, the **alpine** image scores **48/100, grade D (porous)** on IronClaw's seven-dimension container containment scale. Higher is safer. This is what you get straight out of a copy-pasted `docker run`; the fixes below close the gap.

> Graded from a read-only `docker inspect` of `alpine:3.21` at digest `sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d`. No workload is executed. [How scoring works &rarr;](../scan.md)

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

Applying these to your `docker run alpine` closes the biggest gaps first (most points recovered first):

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
docker run -d --name alpine-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  alpine:3.21
```

## Scan your own container

These grades come from `ironctl scan`, a single, credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just this image:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own alpine the same way this page was generated
ironctl scan my-alpine
```

- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey this directory is built from.
- [Run untrusted code in a real sandbox &rarr;](../index.md), IronClaw wraps every AI-agent session in a gVisor/Kata isolation boundary with `network=none` by default.

---

*Part of the [Container Isolation Scores](index.md) directory, default-configuration containment grades for the most-pulled public images.*
