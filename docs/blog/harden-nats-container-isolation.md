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

### Failing Dimensions:

* **Non-root user (uid != 0):** `0/15` - Runs as root (`user "0 (default)"`).
* **Dropped capabilities:** `4/20` - Default capability set retained (`CAP_NET_RAW`, `CAP_MKNOD`, etc.).
* **Read-only root filesystem:** `0/10` - Root filesystem is writable.
* **Network isolation / egress:** `4/15` (WARN) - `network=bridge` allows outbound network egress.

---

## Hardened Configuration Stanza

To fix these findings, apply the following security contexts to drop unnecessary Linux capabilities, enforce a non-root user, and set the root filesystem to read-only.

### Docker Compose Example

```yaml
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