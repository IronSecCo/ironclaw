---
title: "Most secure database container images, ranked by isolation score"
description: "Ranked isolation scores for 38 database container images, graded 0-100 by ironctl scan. Best 63/100, average 51/100. See which ship hardened."
---

# Most secure database container images, ranked by isolation score

How isolated are the most-pulled **database** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **38 relational, document, key-value, and time-series databases (Postgres, MySQL, MongoDB, Redis)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 63/100** (average **51/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No database image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`adminer:4.8.1`](../adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`apache/druid:31.0.0`](../druid.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`hazelcast/hazelcast:5.5.0`](../hazelcast.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`memcached:1.6-alpine`](../memcached.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`percona:8.0`](../percona.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`dpage/pgadmin4:8`](../pgadmin4.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`redis/redisinsight:latest`](../redisinsight.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`aerospike/aerospike-server:7.2.0.1`](../aerospike-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 9 | [`arangodb:3.12`](../arangodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`cassandra:5.0`](../cassandra.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`clickhouse:24.8`](../clickhouse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`cockroachdb/cockroach:latest`](../cockroach.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`couchdb:3.4`](../couchdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`dgraph/dgraph:v24.0.5`](../dgraph.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 15 | [`docker.dragonflydb.io/dragonflydb/dragonfly:latest`](../dragonfly.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 16 | [`firebirdsql/firebird:5.0.1`](../firebird.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 17 | [`apacheignite/ignite:2.16.0`](../ignite.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 18 | [`influxdb:2.7-alpine`](../influxdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 19 | [`eqalpha/keydb:latest`](../keydb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 20 | [`mariadb:11`](../mariadb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 21 | [`mongo:7`](../mongo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 22 | [`mongo-express:1.0`](../mongo-express.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 23 | [`mysql:8.4`](../mysql.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 24 | [`neo4j:5`](../neo4j.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 25 | [`orientdb:3.2.35`](../orientdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 26 | [`phpmyadmin:5.2`](../phpmyadmin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 27 | [`postgis/postgis:17-3.5`](../postgis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 28 | [`postgres:17-alpine`](../postgres.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 29 | [`questdb/questdb:8.2.1`](../questdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 30 | [`redis:7-alpine`](../redis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 31 | [`redis/redis-stack-server:7.4.0-v3`](../redis-stack-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 32 | [`rethinkdb:2.4`](../rethinkdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 33 | [`scylladb/scylla:6.2`](../scylla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 34 | [`tarantool/tarantool:3.3.0`](../tarantool.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 35 | [`pingcap/tidb:v8.5.0`](../tidb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 36 | [`timescale/timescaledb:2.17.2-pg17`](../timescaledb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 37 | [`valkey/valkey:8`](../valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 38 | [`yugabytedb/yugabyte:2.23.0.0-b710`](../yugabyte.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own database container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the database images ranked above:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your own container the same way this page was generated
ironctl scan my-container
```

- [All container isolation scores &rarr;](../index.md), every scorecard, worst-isolated first.
- [Browse every category &rarr;](index.md), the full set of ranked collection pages.
- [Container Isolation Leaderboard &rarr;](../leaderboard.md), the whole dataset ranked, with a Hall of Fame and worst offenders.
- [Scan any container &rarr;](../../scan.md), the full command reference.
- [The State of Container Isolation, 2026 &rarr;](../../blog/state-of-container-isolation-2026.md), the survey these grades are built from.

---

*Part of the [Container Isolation Scores](../index.md) directory. Generated from a reproducible survey by `examples/isolation-survey/gen_scorecards.py` and refreshed weekly, so this ranking never goes stale. Grades reflect each image's default configuration, not a limit of the image itself: every one reaches grade A with the right `docker run` flags.*
