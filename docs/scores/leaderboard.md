---
title: "Container Isolation Leaderboard: the most (and least) isolated Docker images"
description: A ranked leaderboard of the default container isolation score for 252 of the most-pulled public Docker images, graded 0-100 by ironctl scan. Hall of Fame and worst offenders included.
---

# Container Isolation Leaderboard

Every one of **252 of the most-pulled public Docker images**, ranked by how isolated it is when you `docker run` it with plain defaults, no hardening flags. Scores run **48/100 to 63/100** (average **52/100**), graded across IronClaw's seven containment dimensions by `ironctl scan`. Higher is safer. Regenerated with the [weekly survey refresh](index.md), so this ranking never goes stale.

> The uncomfortable headline: **no popular image ships isolated.** Even the leaders leave capabilities, egress, and a writable root filesystem wide open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## 🏆 Hall of Fame

No image ships at grade A, so this is the next best thing: the **15 best-isolated images by default**, the ones that start you closest to a hardened posture.

| Rank | Image | Score | Grade | Remaining gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------------|
| 🥇 1 | [`ghcr.io/actions/actions-runner:2.321.0`](actions-runner.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`apache/activemq-artemis:2.38.0`](activemq-artemis.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`apache/airflow:2.10.4`](airflow.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`quay.io/argoproj/argocd:v2.13.2`](argocd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`quay.io/jupyter/base-notebook:latest`](base-notebook.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`confluentinc/cp-kafka:7.8.0`](cp-kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 9 | [`dexidp/dex:v2.41.1`](dex.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 10 | [`directus/directus:11.3.5`](directus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 11 | [`apache/druid:31.0.0`](druid.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 12 | [`elasticsearch:8.16.1`](elasticsearch.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 13 | [`emqx:5`](emqx.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 14 | [`fluent/fluentd:v1.17`](fluentd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 15 | [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |

## 🚨 Worst offenders

The **15 least-isolated images** in the survey. Pulling one of these unhardened puts a container escape one step from host uid 0. Each links to the exact flags that fix it.

| # | Image | Score | Grade | Top gaps (default config) |
|--:|-------|------:|:-----:|---------------------------|
| 1 | [`azul/zulu-openjdk:21`](zulu-openjdk.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 2 | [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 3 | [`yugabytedb/yugabyte:2.23.0.0-b710`](yugabyte.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 4 | [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 5 | [`lscr.io/linuxserver/wireguard:1.0.20210914`](wireguard.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 6 | [`semitechnologies/weaviate:1.28.1`](weaviate.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 7 | [`wallabag/wallabag:2.6.10`](wallabag.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 8 | [`victoriametrics/victoria-metrics:v1.107.0`](victoria-metrics.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 9 | [`timberio/vector:0.43.0-alpine`](vector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`valkey/valkey:8`](valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`louislam/uptime-kuma:1`](uptime-kuma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`unit:1.34.1-minimal`](unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 15 | [`typesense/typesense:27.1`](typesense.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Best in each category

The most-isolated default image in every family. Comparing like with like, a database against a database, a base OS against a base OS:

| Category | Best-isolated image | Score | Grade |
|----------|---------------------|------:|:-----:|
| Base OS images | [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** |
| Language runtimes | [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** |
| Databases, caches, and stores | [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** |
| Web servers, proxies, and apps | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** |
| Platform and infra services | [`ghcr.io/actions/actions-runner:2.321.0`](actions-runner.md) | 63/100 | 🟡 **C** |

## Full ranking, best to worst

Every graded image, most-isolated first. Click any image for its per-dimension breakdown, the exact hardening flags, and a copy-paste score badge you can embed in your repo.

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`ghcr.io/actions/actions-runner:2.321.0`](actions-runner.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`apache/activemq-artemis:2.38.0`](activemq-artemis.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`apache/airflow:2.10.4`](airflow.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`quay.io/argoproj/argocd:v2.13.2`](argocd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`quay.io/jupyter/base-notebook:latest`](base-notebook.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`confluentinc/cp-kafka:7.8.0`](cp-kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 9 | [`dexidp/dex:v2.41.1`](dex.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 10 | [`directus/directus:11.3.5`](directus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 11 | [`apache/druid:31.0.0`](druid.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 12 | [`elasticsearch:8.16.1`](elasticsearch.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 13 | [`emqx:5`](emqx.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 14 | [`fluent/fluentd:v1.17`](fluentd.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 15 | [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 16 | [`graylog/graylog:6.1`](graylog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 17 | [`groovy:4`](groovy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 18 | [`haproxy:3.1-alpine`](haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 19 | [`goharbor/harbor-core:v2.12.0`](harbor-core.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 20 | [`hazelcast/hazelcast:5.5.0`](hazelcast.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 21 | [`oryd/hydra:v2.2.0`](hydra.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 22 | [`jaegertracing/jaeger:2.1.0`](jaeger.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 23 | [`jenkins/jenkins:lts`](jenkins.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 24 | [`jetty:12-jre21`](jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 25 | [`apache/kafka:3.9.0`](kafka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 26 | [`quay.io/keycloak/keycloak:26.0.7`](keycloak.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 27 | [`kibana:8.16.1`](kibana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 28 | [`kong:3.8`](kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 29 | [`logstash:8.16.1`](logstash.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 30 | [`grafana/loki:3.3.2`](loki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 31 | [`mailhog/mailhog:v1.0.1`](mailhog.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 32 | [`mattermost/mattermost-team-edition:10.2`](mattermost-team-edition.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 33 | [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 34 | [`miniflux/miniflux:2.2.3`](miniflux.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 35 | [`n8nio/n8n:1.71.3`](n8n.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 36 | [`sonatype/nexus3:3.75.0`](nexus3.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 37 | [`nginxinc/nginx-unprivileged:1.27-alpine`](nginx-unprivileged.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 38 | [`apache/nifi:2.0.0`](nifi.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 39 | [`prom/node-exporter:v1.8.2`](node-exporter.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 40 | [`opensearchproject/opensearch:2`](opensearch.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 41 | [`opensearchproject/opensearch-dashboards:2.18.0`](opensearch-dashboards.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 42 | [`outlinewiki/outline:0.82.0`](outline.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 43 | [`percona:8.0`](percona.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 44 | [`dpage/pgadmin4:8`](pgadmin4.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 45 | [`ghcr.io/plankanban/planka:1.24.2`](planka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 46 | [`prom/prometheus:v3.1.0`](prometheus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 47 | [`apachepulsar/pulsar:3.3.2`](pulsar.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 48 | [`prom/pushgateway:v1.10.0`](pushgateway.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 49 | [`redis/redisinsight:latest`](redisinsight.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 50 | [`redpandadata/redpanda:v24.2.11`](redpanda.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 51 | [`payara/server-full:6.2024.12`](server-full.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 52 | [`solr:9`](solr.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 53 | [`sonarqube:community`](sonarqube.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 54 | [`apache/spark:3.5.3`](spark.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 55 | [`selenium/standalone-chrome:4.27.0`](standalone-chrome.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 56 | [`apache/superset:4.1.1`](superset.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 57 | [`jetbrains/teamcity-server:2024.12`](teamcity-server.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 58 | [`grafana/tempo:2.6.1`](tempo.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 59 | [`quay.io/thanos/thanos:v0.37.2`](thanos.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 60 | [`apache/tika:latest`](tika.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 61 | [`ghcr.io/umami-software/umami:postgresql-v2.14.0`](umami.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 62 | [`varnish:7.6-alpine`](varnish.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 63 | [`verdaccio/verdaccio:6`](verdaccio.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 64 | [`vernemq/vernemq:2.0.1`](vernemq.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 65 | [`vespaengine/vespa:8.453.24`](vespa.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 66 | [`wekanteam/wekan:v7.72`](wekan.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 67 | [`requarks/wiki:2`](wiki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 68 | [`apache/zeppelin:0.11.2`](zeppelin.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 69 | [`openzipkin/zipkin:3.4.4`](zipkin.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 70 | [`gitea/act_runner:0.2.11`](act_runner.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 71 | [`actualbudget/actual-server:24.12.0`](actual-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 72 | [`adguard/adguardhome:latest`](adguardhome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 73 | [`aerospike/aerospike-server:7.2.0.1`](aerospike-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 74 | [`grafana/alloy:latest`](alloy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 75 | [`almalinux:9`](almalinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 76 | [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 77 | [`amazoncorretto:21`](amazoncorretto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 78 | [`amazonlinux:2023`](amazonlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 79 | [`continuumio/anaconda3:2024.10-1`](anaconda3.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 80 | [`appwrite/appwrite:1.6.0`](appwrite.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 81 | [`arangodb:3.12`](arangodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 82 | [`archlinux:latest`](archlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 83 | [`mcr.microsoft.com/dotnet/aspnet:9.0`](aspnet.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 84 | [`ghcr.io/advplyr/audiobookshelf:2.17.5`](audiobookshelf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 85 | [`authelia/authelia:4.38`](authelia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 86 | [`baserow/baserow:1.30.1`](baserow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 87 | [`prom/blackbox-exporter:v0.25.0`](blackbox-exporter.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 88 | [`oven/bun:1.1`](bun.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 89 | [`busybox:1.37`](busybox.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 90 | [`caddy:2-alpine`](caddy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 91 | [`gcr.io/cadvisor/cadvisor:v0.52.1`](cadvisor.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 92 | [`cassandra:5.0`](cassandra.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 93 | [`centrifugo/centrifugo:v5.4.7`](centrifugo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 94 | [`quay.io/ceph/ceph:v18.2.4`](ceph.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 95 | [`chromadb/chroma:0.5.23`](chroma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 96 | [`chronograf:1.10`](chronograf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 97 | [`clickhouse:24.8`](clickhouse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 98 | [`clojure:temurin-21-tools-deps`](clojure.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 99 | [`cockroachdb/cockroach:latest`](cockroach.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 100 | [`concourse/concourse:7.12.0`](concourse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 101 | [`hashicorp/consul:1.20`](consul.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 102 | [`quay.io/cortexproject/cortex:v1.18.1`](cortex.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 103 | [`couchdb:3.4`](couchdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 104 | [`crystallang/crystal:1.14.1`](crystal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 105 | [`dart:3.6`](dart.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 106 | [`lissy93/dashy:3.1.0`](dashy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 107 | [`debian:12-slim`](debian.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 108 | [`denoland/deno:2.1.4`](deno.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 109 | [`dgraph/dgraph:v24.0.5`](dgraph.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 110 | [`docker:27-dind`](docker.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 111 | [`docker.dragonflydb.io/dragonflydb/dragonfly:latest`](dragonfly.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 112 | [`drone/drone:2`](drone.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 113 | [`drupal:11-apache`](drupal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 114 | [`lscr.io/linuxserver/duplicati:2.1.0`](duplicati.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 115 | [`eclipse-mosquitto:2`](eclipse-mosquitto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 116 | [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 117 | [`elixir:1.18-alpine`](elixir.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 118 | [`envoyproxy/envoy:v1.32-latest`](envoy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 119 | [`erlang:27-alpine`](erlang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 120 | [`fedora:41`](fedora.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 121 | [`firebirdsql/firebird:5.0.1`](firebird.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 122 | [`flink:1.20`](flink.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 123 | [`codeberg.org/forgejo/forgejo:9`](forgejo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 124 | [`freshrss/freshrss:1.24.3`](freshrss.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 125 | [`gcc:14`](gcc.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 126 | [`ghost:5-alpine`](ghost.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 127 | [`gitea/gitea:1.22`](gitea.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 128 | [`gitlab/gitlab-ce:17.7.0-ce.0`](gitlab-ce.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 129 | [`gitlab/gitlab-runner:v17.7.0`](gitlab-runner.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 130 | [`gogs/gogs:0.13`](gogs.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 131 | [`golang:1.23-alpine`](golang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 132 | [`ghcr.io/graalvm/graalvm-community:21`](graalvm-community.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 133 | [`gradle:8-jdk21`](gradle.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 134 | [`hasura/graphql-engine:v2.44.0`](graphql-engine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 135 | [`haskell:9.8`](haskell.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 136 | [`lscr.io/linuxserver/heimdall:2.6.3`](heimdall.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 137 | [`ghcr.io/home-assistant/home-assistant:2024.12`](home-assistant.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 138 | [`ghcr.io/gethomepage/homepage:v0.10.9`](homepage.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 139 | [`httpd:2.4-alpine`](httpd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 140 | [`ibmjava:8-jre`](ibmjava.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 141 | [`apacheignite/ignite:2.16.0`](ignite.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 142 | [`ghcr.io/immich-app/immich-server:v1.123.0`](immich-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 143 | [`influxdb:2.7-alpine`](influxdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 144 | [`jellyfin/jellyfin:10.10.3`](jellyfin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 145 | [`joomla:5-apache`](joomla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 146 | [`julia:1.11`](julia.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 147 | [`jupyterhub/jupyterhub:5.2.1`](jupyterhub.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 148 | [`rancher/k3s:v1.31.4-k3s1`](k3s.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 149 | [`kapacitor:1.7`](kapacitor.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 150 | [`eqalpha/keydb:latest`](keydb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 151 | [`opensuse/leap:15.6`](leap.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 152 | [`localstack/localstack:4`](localstack.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 153 | [`nickblah/lua:5.4`](lua.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 154 | [`axllent/mailpit:latest`](mailpit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 155 | [`manticoresearch/manticore:6.3.6`](manticore.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 156 | [`mariadb:11`](mariadb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 157 | [`matomo:5`](matomo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 158 | [`maven:3.9-eclipse-temurin-21`](maven.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 159 | [`mediawiki:1.43`](mediawiki.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 160 | [`getmeili/meilisearch:v1.11`](meilisearch.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 161 | [`metabase/metabase:v0.52.4`](metabase.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 162 | [`milvusdb/milvus:v2.5.1`](milvus.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 163 | [`minio/minio:latest`](minio.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 164 | [`ghcr.io/mlflow/mlflow:v2.19.0`](mlflow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 165 | [`mongo:7`](mongo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 166 | [`mongo-express:1.0`](mongo-express.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 167 | [`mono:6.12`](mono.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 168 | [`mysql:8.4`](mysql.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 169 | [`nats:2.10-alpine`](nats.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 170 | [`deluan/navidrome:0.54.3`](navidrome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 171 | [`neo4j:5`](neo4j.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 172 | [`netdata/netdata:stable`](netdata.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 173 | [`nextcloud:30-apache`](nextcloud.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 174 | [`nginx:1.27-alpine`](nginx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 175 | [`nginxproxy/nginx-proxy:1.6.4`](nginx-proxy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 176 | [`jc21/nginx-proxy-manager:2.12.1`](nginx-proxy-manager.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 177 | [`nimlang/nim:2.2.0`](nim.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 178 | [`nocodb/nocodb:0.257.2`](nocodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 179 | [`node:22-alpine`](node.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 180 | [`hashicorp/nomad:1.9`](nomad.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 181 | [`nsqio/nsq:v1.3.0`](nsq.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 182 | [`binwiederhier/ntfy:v2.11.0`](ntfy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 183 | [`ollama/ollama:0.5.4`](ollama.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 184 | [`openresty/openresty:alpine`](openresty.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 185 | [`oraclelinux:9`](oraclelinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 186 | [`orientdb:3.2.35`](orientdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 187 | [`hashicorp/packer:1.11.2`](packer.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 188 | [`ghcr.io/paperless-ngx/paperless-ngx:2.14.7`](paperless-ngx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 189 | [`perl:5.40-slim`](perl.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 190 | [`pgvector/pgvector:pg17`](pgvector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 191 | [`photon:5.0`](photon.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 192 | [`php:8.4-apache`](php.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 193 | [`phpmyadmin:5.2`](phpmyadmin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 194 | [`pihole/pihole:2024.07.0`](pihole.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 195 | [`postgis/postgis:17-3.5`](postgis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 196 | [`postgres:17-alpine`](postgres.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 197 | [`grafana/promtail:3.3.2`](promtail.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 198 | [`pulumi/pulumi:3.144.1`](pulumi.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 199 | [`pypy:3.10-slim`](pypy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 200 | [`python:3.13-alpine`](python.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 201 | [`qdrant/qdrant:v1.12.4`](qdrant.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 202 | [`questdb/questdb:8.2.1`](questdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 203 | [`rabbitmq:4-alpine`](rabbitmq.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 204 | [`rancher/rancher:v2.10.1`](rancher.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 205 | [`rclone/rclone:1.68.2`](rclone.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 206 | [`redis:7-alpine`](redis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 207 | [`redis/redis-stack-server:7.4.0-v3`](redis-stack-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 208 | [`redmine:6`](redmine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 209 | [`registry:2.8.3`](registry.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 210 | [`restic/rest-server:0.13.0`](rest-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 211 | [`rethinkdb:2.4`](rethinkdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 212 | [`rockylinux:9`](rockylinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 213 | [`rocker/rstudio:4.4.2`](rstudio.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 214 | [`ruby:3.4-alpine`](ruby.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 215 | [`rust:1.83-alpine`](rust.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 216 | [`sbtscala/scala-sbt:eclipse-temurin-21.0.5_11_1.10.7_3.6.2`](scala-sbt.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 217 | [`scylladb/scylla:6.2`](scylla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 218 | [`chrislusf/seaweedfs:3.80`](seaweedfs.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 219 | [`vaultwarden/server:1.32.7`](server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 220 | [`snipe/snipe-it:v7.0.13`](snipe-it.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 221 | [`statsd/statsd:v0.10.2`](statsd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 222 | [`storm:2.7.1`](storm.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 223 | [`lscr.io/linuxserver/swag:4.1.0`](swag.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 224 | [`swaggerapi/swagger-ui:latest`](swagger-ui.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 225 | [`swift:6.0.3`](swift.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 226 | [`matrixdotorg/synapse:v1.121.1`](synapse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 227 | [`lscr.io/linuxserver/syncthing:1.28.1`](syncthing.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 228 | [`tailscale/tailscale:v1.78.3`](tailscale.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 229 | [`tarantool/tarantool:3.3.0`](tarantool.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 230 | [`telegraf:1.33-alpine`](telegraf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 231 | [`tensorflow/tensorflow:2.18.0`](tensorflow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 232 | [`hashicorp/terraform:1.10`](terraform.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 233 | [`pingcap/tidb:v8.5.0`](tidb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 234 | [`timescale/timescaledb:2.17.2-pg17`](timescaledb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 235 | [`tomcat:10.1-jre21-temurin`](tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 236 | [`tomee:9.1.3-jre17`](tomee.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 237 | [`traefik:v3.2`](traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 238 | [`typesense/typesense:27.1`](typesense.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 239 | [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 240 | [`unit:1.34.1-minimal`](unit.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 241 | [`louislam/uptime-kuma:1`](uptime-kuma.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 242 | [`valkey/valkey:8`](valkey.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 243 | [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 244 | [`timberio/vector:0.43.0-alpine`](vector.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 245 | [`victoriametrics/victoria-metrics:v1.107.0`](victoria-metrics.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 246 | [`wallabag/wallabag:2.6.10`](wallabag.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 247 | [`semitechnologies/weaviate:1.28.1`](weaviate.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 248 | [`lscr.io/linuxserver/wireguard:1.0.20210914`](wireguard.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 249 | [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 250 | [`yugabytedb/yugabyte:2.23.0.0-b710`](yugabyte.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 251 | [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 252 | [`azul/zulu-openjdk:21`](zulu-openjdk.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Move up the leaderboard

Every gap on this page closes with `docker run` flags. Audit your own container, or one you maintain, with the same credential-free command that produced these grades:

```bash
brew install ironsecco/ironclaw/ironclaw
ironctl scan my-container
```

- [Container Isolation Scores directory](index.md), every scorecard, worst-isolated first.
- [Scores by category](collections/index.md), ranked collection pages for databases, language runtimes, web servers, CI/CD, and more.
- [Scan any container &rarr;](../scan.md), the full command reference.
- [Add an isolation-score badge to your repo &rarr;](../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)
- [The State of Container Isolation, 2026 &rarr;](../blog/state-of-container-isolation-2026.md), the full survey.

---

*Generated from a reproducible survey by `examples/isolation-survey/gen_scorecards.py`. Grades reflect each image's default configuration, not a limit of the image itself: every one reaches grade A with the right `docker run` flags.*
