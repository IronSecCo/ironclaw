---
title: "IronClaw blog: evidence-backed writing on agent containment"
description: "Long-form, evidence-backed posts on securing autonomous AI agents. Every claim links to shipped code, a versioned threat model, or a re-runnable benchmark."
---

# IronClaw blog

Long-form writing on running autonomous AI agents safely. Every post links its claims to
shipped code, a versioned threat model, or a benchmark you can re-run yourself. No
adjectives standing in for numbers.

## Posts

- [State of Container Isolation 2026: we graded 16 popular images and 15 scored a D or worse](state-of-container-isolation-2026.md) -
  a reproducible survey of 16 popular public images in their common run
  configurations, graded 0 to 100 with `ironctl scan`. Only one hit an A;
  thirteen landed on a D. The median default image scores 48 of 100, running as
  root with the full capability set and a writable root filesystem.
- [We ran the same escape suite against Docker, gVisor, E2B, and Daytona](containment-benchmark-docker-gvisor-e2b-daytona.md) -
  a reproducible containment benchmark. One fixed escape-attempt suite, scored by
  observed behavior. Raw Docker blocked 2 of 5, hardened runc 4 of 5, gVisor 5 of 5,
  with honest labels for the hosted platforms.
- [Audit your sandbox in 10 seconds with ironctl scan](audit-your-sandbox-in-10-seconds.md) -
  one command grades any container, compose service, or Kubernetes pod on a 0 to 100
  containment scale. Fail-closed, works on your own setups. A wide-open container scores
  23 of 100; a hardened IronClaw sandbox scores 100.
- [Add a live Sandbox Isolation Score badge to your repo](add-a-sandbox-isolation-score-badge-to-your-repo.md) -
  generate a shields.io endpoint JSON with `ironctl scan --badge-json`, commit it as a
  static file, and render a live 0 to 100 A-to-F containment grade in your README. No
  server, no scan on every badge hit.
- [IronClaw scan is now a GitHub Action on the Marketplace](ironclaw-scan-github-action-marketplace.md) -
  add `uses: IronSecCo/ironclaw@v1` to any workflow and every pull request gets a 0 to
  100 sandbox isolation scorecard as a sticky comment. Same grader as the CLI: local,
  read-only, credential-free.

## Hardening guides

Per-image, data-driven walkthroughs: the default isolation grade, the exact dimensions that
fail, and the precise `ironctl scan --fix` flags that close the gap, with before and after scores.

- [How to harden a Postgres container](harden-postgres-container-isolation.md) -
  `postgres:17-alpine` scores 48 of 100 (D) on defaults: root, full capabilities, writable
  rootfs. Four flags take it to 100 of 100 (A). Is it safe for untrusted workloads?
- [How to harden a Redis container](harden-redis-container-isolation.md) -
  `redis:7-alpine` scores 48 of 100 (D). A read-only rootfs and dropped capabilities take the
  classic `CONFIG SET dir` file-write RCE off the table before auth. To 100 of 100 (A).
- [How to run untrusted Node.js code safely](run-untrusted-nodejs-code-safely.md) -
  the container is the sandbox, not the `vm` module. `node:22-alpine` is 48 of 100 (D) by
  default; `network=none` plus five flags make it a 100 of 100 (A) boundary for untrusted JS.
- [How to harden an nginx container](harden-nginx-container-isolation.md) -
  `nginx:1.27-alpine` scores 48 of 100 (D). The honest hardened ceiling for an internet-facing
  proxy is 89 of 100 (B), because it must reach its upstreams. Here is exactly why.
- [How to harden a MySQL container](harden-mysql-container-isolation.md) -
  `mysql:8.4` scores 48 of 100 (D) on defaults: root, full capabilities, writable rootfs. Four
  flags take it to 100 of 100 (A). Is it safe for untrusted workloads?
- [How to harden an Elasticsearch container](harden-elasticsearch-container-isolation.md) -
  `elasticsearch:8.16.1` already runs non-root, so it starts at 63 of 100 (C). Three flags take a
  single-node index to 100 of 100 (A); a multi-node cluster has an honest 89 of 100 (B) ceiling.
- [How to harden a RabbitMQ container](harden-rabbitmq-container-isolation.md) -
  `rabbitmq:4-alpine` scores 48 of 100 (D). The honest hardened ceiling for a broker is 89 of 100
  (B), because clients must be able to connect to it. Here is exactly why.
- [How to harden a Memcached container](harden-memcached-container-isolation.md) -
  `memcached:1.6-alpine` runs non-root and holds nothing on disk, so it starts at 63 of 100 (C).
  A read-only rootfs (no volume needed) and dropped capabilities take it to its honest 89 of 100 (B).
- [How to harden a Cassandra container](harden-cassandra-container-isolation.md) -
  `cassandra:5.0` scores 48 of 100 (D) on defaults: root, full capabilities, writable rootfs. Four
  flags take a single-node ring to 100 of 100 (A); a gossiping cluster has an honest 89 of 100 (B) ceiling.
- [How to harden a ClickHouse container](harden-clickhouse-container-isolation.md) -
  `clickhouse:24.8` scores 48 of 100 (D). Cutting egress closes its remote-table reach; four flags
  take the analytics store to 100 of 100 (A).
- [How to harden a Consul container](harden-consul-container-isolation.md) -
  `hashicorp/consul:1.20` scores 48 of 100 (D). The honest hardened ceiling for a service-mesh agent
  is 89 of 100 (B), because peers and clients must connect. Unlike Vault, it does not mlock by default.
- [How to harden a MinIO container](harden-minio-container-isolation.md) -
  `minio/minio` scores 48 of 100 (D). The honest hardened ceiling for an object store is 89 of 100
  (B), because S3 clients must reach the API. Here is exactly why.
- [How to harden a Grafana container](harden-grafana-container-isolation.md) -
  `grafana/grafana:11.2.0` already runs non-root, so it starts at 63 of 100 (C). The honest hardened
  ceiling for a dashboard server is 89 of 100 (B), because browsers must reach the UI.
- [How to harden a Prometheus container](harden-prometheus-container-isolation.md) -
  `prom/prometheus:v3.1.0` runs as `nobody`, so it starts at 63 of 100 (C). The honest hardened ceiling
  for a metrics server is 89 of 100 (B), because it must scrape targets and serve its API.
- [How to harden a Traefik container](harden-traefik-container-isolation.md) -
  `traefik:v3.2` scores 48 of 100 (D). The honest hardened ceiling for an edge reverse proxy is 89 of
  100 (B), because it exists to accept and forward traffic. Here is exactly why.
- [How to harden an InfluxDB container](harden-influxdb-container-isolation.md) -
  `influxdb:2.7` scores 48 of 100 (D). Four flags take a co-located time-series store to 100 of 100
  (A); a fleet pushing metrics over the network has an honest 89 of 100 (B) ceiling.
- [How to scan a Dockerfile for security issues](scan-a-dockerfile-for-security-issues.md) -
  a deliberately bad Dockerfile (root default, unpinned base, a baked-in secret) scores 5 of
  100 (F) on a static, daemon-free scan. The exact one-line fixes take it to 100 of 100 (A).

## Comparisons

Head-to-head reads, each backed by the same scan data, for the questions people
actually search.

- [Alpine vs Debian vs Ubuntu: does your base image change container isolation?](alpine-vs-debian-vs-ubuntu-container-isolation.md) -
  we scored the seven most-pulled base OS images. Every one landed on the same 48
  of 100. Base image choice barely moves the isolation needle; runtime flags do.
- [Docker default vs hardened: the container isolation score gap, measured](docker-default-vs-hardened-container-isolation.md) -
  151 images averaged 52 of 100 on defaults and 100 of 100 hardened. The exact
  48-point jump, dimension by dimension, and the six flags that produce it.
- [gVisor vs runc: container isolation compared](gvisor-vs-runc-container-isolation-compared.md) -
  they score identically on a config scan yet block a different number of real
  escape attempts. When hardened runc is enough and when you need a user-space
  kernel.
