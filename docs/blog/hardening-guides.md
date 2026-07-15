---
title: "Container hardening guides: harden Postgres, MySQL, Redis, Kafka, Vault and more"
description: "Every IronClaw harden-a-container walkthrough in one place. Real ironctl scan before/after grades and the exact flags that close the gap, per image."
---

# Container hardening guides

Every guide below takes one popular Docker image, grades its **default** `docker run` on IronClaw's
seven-dimension containment scale, and shows the exact `ironctl scan --fix` flags that close the gap.
The numbers are not hand-waved: each comes from a read-only `docker inspect` of the real image, the
same data behind that image's [isolation scorecard](../scores/index.md). No workload is executed to
produce them.

Two patterns show up across the set:

- **Datastores and sandboxes reach grade A.** A database or a code sandbox that only its co-located
  services talk to can take `--network=none` and hit **100/100**. Root, capabilities, and a
  read-only rootfs are the whole game.
- **Network services hit an honest ceiling at grade B.** A broker, cache, proxy, or secrets server
  *exists to be connected to*, so `--network=none` would score the last points but break the service.
  The honest ceiling is **89/100, grade B**, with the network dimension held at a WARN and contained
  with a scoped private network instead. We say so on every page rather than inflate the number.

## Reach grade A (100/100)

| Service | Default | Hardened | The gap that closes |
|---------|:-------:|:--------:|---------------------|
| [Postgres](harden-postgres-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [MySQL](harden-mysql-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [MariaDB](harden-mariadb-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [MongoDB](harden-mongodb-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [Redis](harden-redis-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [Cassandra](harden-cassandra-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs (89/B multi-node) |
| [ClickHouse](harden-clickhouse-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs |
| [Elasticsearch](harden-elasticsearch-container-isolation.md) | 63/100 C | **100/100 A** | full caps, writable rootfs (89/B multi-node) |
| [InfluxDB](harden-influxdb-container-isolation.md) | 48/100 D | **100/100 A** | root, full caps, writable rootfs (89/B if fleet pushes metrics) |
| [Untrusted Node.js](run-untrusted-nodejs-code-safely.md) | 48/100 D | **100/100 A** | run untrusted code in a real sandbox |

## Honest ceiling: grade B (89/100)

These services must accept client connections, so the network dimension holds at a WARN by design.

| Service | Default | Hardened | Why 89 is the ceiling |
|---------|:-------:|:--------:|-----------------------|
| [Kafka](harden-kafka-container-isolation.md) | 63/100 C | **89/100 B** | broker: producers and consumers must connect |
| [RabbitMQ](harden-rabbitmq-container-isolation.md) | 48/100 D | **89/100 B** | broker: every service connects to the queue |
| [Memcached](harden-memcached-container-isolation.md) | 63/100 C | **89/100 B** | cache: clients read and write over the network |
| [nginx](harden-nginx-container-isolation.md) | 48/100 D | **89/100 B** | proxy: it exists to forward traffic |
| [Vault](harden-vault-container-isolation.md) | 48/100 D | **89/100 B** | secrets server: apps must reach the API |
| [Consul](harden-consul-container-isolation.md) | 48/100 D | **89/100 B** | service mesh: peers and clients must connect |
| [MinIO](harden-minio-container-isolation.md) | 48/100 D | **89/100 B** | object store: S3 clients must reach the API |
| [Grafana](harden-grafana-container-isolation.md) | 63/100 C | **89/100 B** | dashboard: browsers must reach the UI |
| [Prometheus](harden-prometheus-container-isolation.md) | 63/100 C | **89/100 B** | metrics: it scrapes targets and serves its API |
| [Traefik](harden-traefik-container-isolation.md) | 48/100 D | **89/100 B** | proxy: it exists to accept and forward traffic |

## The pattern, one command

Every grade on this page comes from the same tool. Point it at any running container, a
`docker-compose.yml` service, or a Kubernetes manifest, and it grades the real thing, then prints the
fixes:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade it, then print the exact hardening flags
ironctl scan my-container
ironctl scan my-container --fix
```

Want it in CI? The same engine ships as a [GitHub Action on the
Marketplace](ironclaw-scan-github-action-marketplace.md) that scores every pull request and posts the
grade as a sticky comment.

## Keep going

- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Container Isolation Scores &rarr;](../scores/index.md): default-config grades for the most-pulled public images.
- [The State of Container Isolation, 2026 &rarr;](state-of-container-isolation-2026.md): the survey the whole directory is built from.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
