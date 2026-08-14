---
title: How to Harden NATS Container Isolation
description: A practical guide to securing NATS containers using Docker Compose and Kubernetes security contexts.
---

# How to Harden NATS Container Isolation

NATS is a lightweight, high-performance cloud-native messaging system. By default, running stock NATS containers leaves several security dimensions unconfigured.

For details on full scoring metrics, see [NATS Scores](../scores/nats.md).

---

## Stock Container Scan Results

Running an `ironctl` containment scan on an unmodified `nats` container produces the following initial evaluation:

* **Stock Score:** `48/100` (Grade D - porous)

| Dimension | Verdict | Score | What the scan found |
| --- | --- | --- | --- |
| Non-root user (uid != 0) | FAIL | 0/15 | runs as root (user "0 (default)"); a container escape starts with host-uid 0 |
| Dropped capabilities | FAIL | 4/20 | default capability set retained (includes CAP_NET_RAW, CAP_MKNOD, …) |
| Seccomp profile | PASS | 15/15 | seccomp profile active (syscall surface filtered) |
| Network isolation / egress | WARN | 4/15 | network=bridge: outbound egress is possible; prefer network=none |
| Read-only root filesystem | FAIL | 0/10 | root filesystem is writable: tamper/persistence surface |
| No docker.sock exposure | PASS | 15/15 | no docker.sock / OCI control socket mounted |
| No shared host namespaces | PASS | 10/10 | no host PID/IPC/network namespace sharing |

---

## Hardened Configuration Stanza

To fix these findings, apply the following security contexts to drop unnecessary Linux capabilities, enforce a non-root user, and set the root filesystem to read-only.

### Docker Compose Example

yaml
version: '3.8'

services:
  nats:
    image: nats:latest
    container_name: nats-hardened
    user: "10001:10001"
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    tmpfs:
      - /tmp
    ports:
      - "4222:4222"
      - "8222:8222"
    restart: unless-stopped