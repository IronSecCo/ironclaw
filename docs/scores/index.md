---
title: Container Isolation Scores
description: The default-configuration container isolation score for 151+ of the most-pulled public Docker images, graded 0-100 across seven containment dimensions by ironctl scan.
---

# Container Isolation Scores

How isolated is the container you just `docker run`? This directory grades **151 of the most-pulled public images** as they ship, run with plain `docker run <image>` defaults, no hardening flags, on IronClaw's seven-dimension containment scale (0-100). Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

**The headline:** the average default image scores **52/100**. Grade distribution: 38×C, 113×D. Almost nothing you pull is isolated out of the box, it runs as root, keeps the full capability set, and has a writable root filesystem. The good news: every gap on these pages closes with a handful of `docker run` flags.

> **Scan your own container:** `brew install ironsecco/ironclaw/ironclaw && ironctl scan my-container`. See [Scan any container](../scan.md).

> **New:** the [Container Isolation Leaderboard](leaderboard.md) ranks every image best-to-worst, with a Hall of Fame and a Worst-offenders cut. Each scorecard now carries a copy-paste [shields.io badge](leaderboard.md) you can embed in your repo.

## Every image, worst-isolated first

| Image | Score | Grade | Top gaps (default config) |
|-------|------:|:-----:|---------------------------|
| [`adguard/adguardhome:latest`](adguardhome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`grafana/alloy:latest`](alloy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`almalinux:9`](almalinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`amazoncorretto:21`](amazoncorretto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`amazonlinux:2023`](amazonlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`arangodb:3.12`](arangodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`archlinux:latest`](archlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`authelia/authelia:4.38`](authelia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`prom/blackbox-exporter:v0.25.0`](blackbox-exporter.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`oven/bun:1.1`](bun.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`busybox:1.37`](busybox.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`caddy:2-alpine`](caddy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`cassandra:5.0`](cassandra.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`chromadb/chroma:0.5.23`](chroma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`chronograf:1.10`](chronograf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`clickhouse:24.8`](clickhouse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`clojure:temurin-21-tools-deps`](clojure.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`cockroachdb/cockroach:latest`](cockroach.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/consul:1.20`](consul.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`couchdb:3.4`](couchdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`dart:3.6`](dart.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`debian:12-slim`](debian.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`denoland/deno:2.1.4`](deno.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`docker:27-dind`](docker.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`docker.dragonflydb.io/dragonflydb/dragonfly:latest`](dragonfly.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`drone/drone:2`](drone.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`drupal:11-apache`](drupal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`eclipse-mosquitto:2`](eclipse-mosquitto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`elixir:1.18-alpine`](elixir.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`envoyproxy/envoy:v1.32-latest`](envoy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`erlang:27-alpine`](erlang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`fedora:41`](fedora.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`flink:1.20`](flink.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`codeberg.org/forgejo/forgejo:9`](forgejo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`gcc:14`](gcc.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ghost:5-alpine`](ghost.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`gitea/gitea:1.22`](gitea.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`gogs/gogs:0.13`](gogs.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`golang:1.23-alpine`](golang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`gradle:8-jdk21`](gradle.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`haskell:9.8`](haskell.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`httpd:2.4-alpine`](httpd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`influxdb:2.7-alpine`](influxdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`jellyfin/jellyfin:10.10.3`](jellyfin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`joomla:5-apache`](joomla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`julia:1.11`](julia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`kapacitor:1.7`](kapacitor.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`eqalpha/keydb:latest`](keydb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`opensuse/leap:15.6`](leap.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`localstack/localstack:4`](localstack.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`axllent/mailpit:latest`](mailpit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mariadb:11`](mariadb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`matomo:5`](matomo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`maven:3.9-eclipse-temurin-21`](maven.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mediawiki:1.43`](mediawiki.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`getmeili/meilisearch:v1.11`](meilisearch.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`metabase/metabase:v0.52.4`](metabase.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`minio/minio:latest`](minio.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mongo:7`](mongo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mongo-express:1.0`](mongo-express.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mysql:8.4`](mysql.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`nats:2.10-alpine`](nats.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`neo4j:5`](neo4j.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`netdata/netdata:stable`](netdata.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`nextcloud:30-apache`](nextcloud.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`nginx:1.27-alpine`](nginx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`node:22-alpine`](node.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/nomad:1.9`](nomad.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`openresty/openresty:alpine`](openresty.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`oraclelinux:9`](oraclelinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`perl:5.40-slim`](perl.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`pgvector/pgvector:pg17`](pgvector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`photon:5.0`](photon.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`php:8.4-apache`](php.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`phpmyadmin:5.2`](phpmyadmin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`pihole/pihole:2024.07.0`](pihole.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`postgis/postgis:17-3.5`](postgis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`postgres:17-alpine`](postgres.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`grafana/promtail:3.3.2`](promtail.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`pypy:3.10-slim`](pypy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`python:3.13-alpine`](python.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`qdrant/qdrant:v1.12.4`](qdrant.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`questdb/questdb:8.2.1`](questdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rabbitmq:4-alpine`](rabbitmq.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`redis:7-alpine`](redis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`redis/redis-stack-server:7.4.0-v3`](redis-stack-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`redmine:6`](redmine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`registry:2.8.3`](registry.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rethinkdb:2.4`](rethinkdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rockylinux:9`](rockylinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ruby:3.4-alpine`](ruby.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rust:1.83-alpine`](rust.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`scylladb/scylla:6.2`](scylla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`vaultwarden/server:1.32.7`](server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`snipe/snipe-it:v7.0.13`](snipe-it.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`swaggerapi/swagger-ui:latest`](swagger-ui.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`telegraf:1.33-alpine`](telegraf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/terraform:1.10`](terraform.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`tomcat:10.1-jre21-temurin`](tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`traefik:v3.2`](traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`typesense/typesense:27.1`](typesense.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`unit:1.34.1-minimal`](unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`louislam/uptime-kuma:1`](uptime-kuma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`valkey/valkey:8`](valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`timberio/vector:0.43.0-alpine`](vector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`victoriametrics/victoria-metrics:v1.107.0`](victoria-metrics.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`semitechnologies/weaviate:1.28.1`](weaviate.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`confluentinc/cp-kafka:7.8.0`](cp-kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`emqx:5`](emqx.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`fluent/fluentd:v1.17`](fluentd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`graylog/graylog:6.1`](graylog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`groovy:4`](groovy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`haproxy:3.1-alpine`](haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`oryd/hydra:v2.2.0`](hydra.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`jaegertracing/jaeger:2.1.0`](jaeger.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`jenkins/jenkins:lts`](jenkins.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`jetty:12-jre21`](jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`apache/kafka:3.9.0`](kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`kong:3.8`](kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`grafana/loki:3.3.2`](loki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`mailhog/mailhog:v1.0.1`](mailhog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`mattermost/mattermost-team-edition:10.2`](mattermost-team-edition.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`n8nio/n8n:1.71.3`](n8n.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`sonatype/nexus3:3.75.0`](nexus3.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`nginxinc/nginx-unprivileged:1.27-alpine`](nginx-unprivileged.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/node-exporter:v1.8.2`](node-exporter.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`opensearchproject/opensearch:2`](opensearch.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`percona:8.0`](percona.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`dpage/pgadmin4:8`](pgadmin4.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/prometheus:v3.1.0`](prometheus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`apachepulsar/pulsar:3.3.2`](pulsar.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/pushgateway:v1.10.0`](pushgateway.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`redis/redisinsight:latest`](redisinsight.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`redpandadata/redpanda:v24.2.11`](redpanda.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`solr:9`](solr.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`sonarqube:community`](sonarqube.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`grafana/tempo:2.6.1`](tempo.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`apache/tika:latest`](tika.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`varnish:7.6-alpine`](varnish.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`verdaccio/verdaccio:6`](verdaccio.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`requarks/wiki:2`](wiki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |

## What the seven dimensions mean

Each image is graded on the same containment dimensions IronClaw's own sandbox benchmark checks, the properties that decide whether a container escape starts from a strong or a hopeless position:

- **Non-root user** (15 pts), does it drop host uid 0?
- **Dropped capabilities** (20 pts), is the Linux capability set minimized?
- **Seccomp profile** (15 pts), is the syscall surface filtered?
- **Network isolation** (15 pts), is egress cut (`network=none`)?
- **Read-only root filesystem** (10 pts), is the tamper surface removed?
- **No docker.sock exposure** (15 pts), is the host control socket kept out?
- **No shared host namespaces** (10 pts), are PID/IPC/net namespaces private?

Scores are fail-closed: any posture the scanner cannot determine is graded as insecure, never silently passed. See [how scoring works](../scan.md) and [the full survey methodology](../blog/state-of-container-isolation-2026.md).

---

*These pages are generated from a reproducible survey, `examples/isolation-survey/survey.sh` scans every image, `gen_scorecards.py` renders the pages. Grades reflect the image's default configuration, not a limit of the image itself: every one can reach grade A with the right `docker run` flags.*
