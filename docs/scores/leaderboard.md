---
title: "Container Isolation Leaderboard: the most (and least) isolated Docker images"
description: A ranked leaderboard of the default container isolation score for 151 of the most-pulled public Docker images, graded 0-100 by ironctl scan. Hall of Fame and worst offenders included.
---

# Container Isolation Leaderboard

Every one of **151 of the most-pulled public Docker images**, ranked by how isolated it is when you `docker run` it with plain defaults, no hardening flags. Scores run **48/100 to 63/100** (average **52/100**), graded across IronClaw's seven containment dimensions by `ironctl scan`. Higher is safer. Regenerated with the [weekly survey refresh](index.md), so this ranking never goes stale.

> The uncomfortable headline: **no popular image ships isolated.** Even the leaders leave capabilities, egress, and a writable root filesystem wide open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## 🏆 Hall of Fame

No image ships at grade A, so this is the next best thing: the **15 best-isolated images by default**, the ones that start you closest to a hardened posture.

| Rank | Image | Score | Grade | Remaining gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------------|
| 🥇 1 | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`confluentinc/cp-kafka:7.8.0`](cp-kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`emqx:5`](emqx.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`fluent/fluentd:v1.17`](fluentd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`graylog/graylog:6.1`](graylog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`groovy:4`](groovy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 9 | [`haproxy:3.1-alpine`](haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 10 | [`oryd/hydra:v2.2.0`](hydra.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 11 | [`jaegertracing/jaeger:2.1.0`](jaeger.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 12 | [`jenkins/jenkins:lts`](jenkins.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 13 | [`jetty:12-jre21`](jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 14 | [`apache/kafka:3.9.0`](kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 15 | [`kong:3.8`](kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |

## 🚨 Worst offenders

The **15 least-isolated images** in the survey. Pulling one of these unhardened puts a container escape one step from host uid 0. Each links to the exact flags that fix it.

| # | Image | Score | Grade | Top gaps (default config) |
|--:|-------|------:|:-----:|---------------------------|
| 1 | [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 2 | [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 3 | [`semitechnologies/weaviate:1.28.1`](weaviate.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 4 | [`victoriametrics/victoria-metrics:v1.107.0`](victoria-metrics.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 5 | [`timberio/vector:0.43.0-alpine`](vector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 6 | [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 7 | [`valkey/valkey:8`](valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 8 | [`louislam/uptime-kuma:1`](uptime-kuma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 9 | [`unit:1.34.1-minimal`](unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`typesense/typesense:27.1`](typesense.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`traefik:v3.2`](traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`tomcat:10.1-jre21-temurin`](tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`hashicorp/terraform:1.10`](terraform.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 15 | [`telegraf:1.33-alpine`](telegraf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Best in each category

The most-isolated default image in every family. Comparing like with like, a database against a database, a base OS against a base OS:

| Category | Best-isolated image | Score | Grade |
|----------|---------------------|------:|:-----:|
| Base OS images | [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** |
| Language runtimes | [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** |
| Databases, caches, and stores | [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** |
| Web servers, proxies, and apps | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** |
| Platform and infra services | [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** |

## Full ranking, best to worst

Every graded image, most-isolated first. Click any image for its per-dimension breakdown, the exact hardening flags, and a copy-paste score badge you can embed in your repo.

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`confluentinc/cp-kafka:7.8.0`](cp-kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`emqx:5`](emqx.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`fluent/fluentd:v1.17`](fluentd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`graylog/graylog:6.1`](graylog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`groovy:4`](groovy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 9 | [`haproxy:3.1-alpine`](haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 10 | [`oryd/hydra:v2.2.0`](hydra.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 11 | [`jaegertracing/jaeger:2.1.0`](jaeger.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 12 | [`jenkins/jenkins:lts`](jenkins.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 13 | [`jetty:12-jre21`](jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 14 | [`apache/kafka:3.9.0`](kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 15 | [`kong:3.8`](kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 16 | [`grafana/loki:3.3.2`](loki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 17 | [`mailhog/mailhog:v1.0.1`](mailhog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 18 | [`mattermost/mattermost-team-edition:10.2`](mattermost-team-edition.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 19 | [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 20 | [`n8nio/n8n:1.71.3`](n8n.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 21 | [`sonatype/nexus3:3.75.0`](nexus3.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 22 | [`nginxinc/nginx-unprivileged:1.27-alpine`](nginx-unprivileged.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 23 | [`prom/node-exporter:v1.8.2`](node-exporter.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 24 | [`opensearchproject/opensearch:2`](opensearch.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 25 | [`percona:8.0`](percona.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 26 | [`dpage/pgadmin4:8`](pgadmin4.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 27 | [`prom/prometheus:v3.1.0`](prometheus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 28 | [`apachepulsar/pulsar:3.3.2`](pulsar.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 29 | [`prom/pushgateway:v1.10.0`](pushgateway.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 30 | [`redis/redisinsight:latest`](redisinsight.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 31 | [`redpandadata/redpanda:v24.2.11`](redpanda.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 32 | [`solr:9`](solr.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 33 | [`sonarqube:community`](sonarqube.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 34 | [`grafana/tempo:2.6.1`](tempo.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 35 | [`apache/tika:latest`](tika.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 36 | [`varnish:7.6-alpine`](varnish.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 37 | [`verdaccio/verdaccio:6`](verdaccio.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 38 | [`requarks/wiki:2`](wiki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 39 | [`adguard/adguardhome:latest`](adguardhome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 40 | [`grafana/alloy:latest`](alloy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 41 | [`almalinux:9`](almalinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 42 | [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 43 | [`amazoncorretto:21`](amazoncorretto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 44 | [`amazonlinux:2023`](amazonlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 45 | [`arangodb:3.12`](arangodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 46 | [`archlinux:latest`](archlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 47 | [`authelia/authelia:4.38`](authelia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 48 | [`prom/blackbox-exporter:v0.25.0`](blackbox-exporter.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 49 | [`oven/bun:1.1`](bun.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 50 | [`busybox:1.37`](busybox.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 51 | [`caddy:2-alpine`](caddy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 52 | [`cassandra:5.0`](cassandra.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 53 | [`chromadb/chroma:0.5.23`](chroma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 54 | [`chronograf:1.10`](chronograf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 55 | [`clickhouse:24.8`](clickhouse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 56 | [`clojure:temurin-21-tools-deps`](clojure.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 57 | [`cockroachdb/cockroach:latest`](cockroach.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 58 | [`hashicorp/consul:1.20`](consul.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 59 | [`couchdb:3.4`](couchdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 60 | [`dart:3.6`](dart.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 61 | [`debian:12-slim`](debian.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 62 | [`denoland/deno:2.1.4`](deno.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 63 | [`docker:27-dind`](docker.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 64 | [`docker.dragonflydb.io/dragonflydb/dragonfly:latest`](dragonfly.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 65 | [`drone/drone:2`](drone.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 66 | [`drupal:11-apache`](drupal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 67 | [`eclipse-mosquitto:2`](eclipse-mosquitto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 68 | [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 69 | [`elixir:1.18-alpine`](elixir.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 70 | [`envoyproxy/envoy:v1.32-latest`](envoy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 71 | [`erlang:27-alpine`](erlang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 72 | [`fedora:41`](fedora.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 73 | [`flink:1.20`](flink.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 74 | [`codeberg.org/forgejo/forgejo:9`](forgejo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 75 | [`gcc:14`](gcc.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 76 | [`ghost:5-alpine`](ghost.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 77 | [`gitea/gitea:1.22`](gitea.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 78 | [`gogs/gogs:0.13`](gogs.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 79 | [`golang:1.23-alpine`](golang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 80 | [`gradle:8-jdk21`](gradle.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 81 | [`haskell:9.8`](haskell.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 82 | [`httpd:2.4-alpine`](httpd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 83 | [`influxdb:2.7-alpine`](influxdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 84 | [`jellyfin/jellyfin:10.10.3`](jellyfin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 85 | [`joomla:5-apache`](joomla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 86 | [`julia:1.11`](julia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 87 | [`kapacitor:1.7`](kapacitor.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 88 | [`eqalpha/keydb:latest`](keydb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 89 | [`opensuse/leap:15.6`](leap.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 90 | [`localstack/localstack:4`](localstack.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 91 | [`axllent/mailpit:latest`](mailpit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 92 | [`mariadb:11`](mariadb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 93 | [`matomo:5`](matomo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 94 | [`maven:3.9-eclipse-temurin-21`](maven.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 95 | [`mediawiki:1.43`](mediawiki.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 96 | [`getmeili/meilisearch:v1.11`](meilisearch.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 97 | [`metabase/metabase:v0.52.4`](metabase.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 98 | [`minio/minio:latest`](minio.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 99 | [`mongo:7`](mongo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 100 | [`mongo-express:1.0`](mongo-express.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 101 | [`mysql:8.4`](mysql.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 102 | [`nats:2.10-alpine`](nats.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 103 | [`neo4j:5`](neo4j.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 104 | [`netdata/netdata:stable`](netdata.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 105 | [`nextcloud:30-apache`](nextcloud.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 106 | [`nginx:1.27-alpine`](nginx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 107 | [`node:22-alpine`](node.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 108 | [`hashicorp/nomad:1.9`](nomad.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 109 | [`openresty/openresty:alpine`](openresty.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 110 | [`oraclelinux:9`](oraclelinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 111 | [`perl:5.40-slim`](perl.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 112 | [`pgvector/pgvector:pg17`](pgvector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 113 | [`photon:5.0`](photon.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 114 | [`php:8.4-apache`](php.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 115 | [`phpmyadmin:5.2`](phpmyadmin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 116 | [`pihole/pihole:2024.07.0`](pihole.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 117 | [`postgis/postgis:17-3.5`](postgis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 118 | [`postgres:17-alpine`](postgres.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 119 | [`grafana/promtail:3.3.2`](promtail.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 120 | [`pypy:3.10-slim`](pypy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 121 | [`python:3.13-alpine`](python.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 122 | [`qdrant/qdrant:v1.12.4`](qdrant.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 123 | [`questdb/questdb:8.2.1`](questdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 124 | [`rabbitmq:4-alpine`](rabbitmq.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 125 | [`redis:7-alpine`](redis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 126 | [`redis/redis-stack-server:7.4.0-v3`](redis-stack-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 127 | [`redmine:6`](redmine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 128 | [`registry:2.8.3`](registry.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 129 | [`rethinkdb:2.4`](rethinkdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 130 | [`rockylinux:9`](rockylinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 131 | [`ruby:3.4-alpine`](ruby.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 132 | [`rust:1.83-alpine`](rust.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 133 | [`scylladb/scylla:6.2`](scylla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 134 | [`vaultwarden/server:1.32.7`](server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 135 | [`snipe/snipe-it:v7.0.13`](snipe-it.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 136 | [`swaggerapi/swagger-ui:latest`](swagger-ui.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 137 | [`telegraf:1.33-alpine`](telegraf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 138 | [`hashicorp/terraform:1.10`](terraform.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 139 | [`tomcat:10.1-jre21-temurin`](tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 140 | [`traefik:v3.2`](traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 141 | [`typesense/typesense:27.1`](typesense.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 142 | [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 143 | [`unit:1.34.1-minimal`](unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 144 | [`louislam/uptime-kuma:1`](uptime-kuma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 145 | [`valkey/valkey:8`](valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 146 | [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 147 | [`timberio/vector:0.43.0-alpine`](vector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 148 | [`victoriametrics/victoria-metrics:v1.107.0`](victoria-metrics.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 149 | [`semitechnologies/weaviate:1.28.1`](weaviate.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 150 | [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 151 | [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Move up the leaderboard

Every gap on this page closes with `docker run` flags. Audit your own container, or one you maintain, with the same credential-free command that produced these grades:

```bash
brew install ironsecco/ironclaw/ironclaw
ironctl scan my-container
```

- [Container Isolation Scores directory](index.md), every scorecard, worst-isolated first.
- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey.

---

*Generated from a reproducible survey by `examples/isolation-survey/gen_scorecards.py`. Grades reflect each image's default configuration, not a limit of the image itself: every one reaches grade A with the right `docker run` flags.*
