# State of Container Isolation — survey results

Scanned **155 scenarios** with `ironctl scan` dev+b8485d9fb1d0 on 2026-07-10T05:17:23Z.

Each row is one popular public image run with a specific configuration, graded 0-100 across seven containment dimensions (non-root user, dropped capabilities, seccomp, network isolation, read-only rootfs, no docker.sock, no host namespaces). Higher is safer. See [README.md](./README.md) for the exact method and [images.txt](./images.txt) for the pinned manifest.

| Scenario | Image | Score | Grade | Top failed dimensions |
|----------|-------|------:|:-----:|-----------------------|
| `naive-privileged` | `python:3.13-alpine` | 19/100 | **F** | Dropped capabilities, Non-root user (uid != 0), Seccomp profile |
| `naive-ci-docker-sock` | `node:22-alpine` | 33/100 | **D** | Dropped capabilities, Non-root user (uid != 0), No docker.sock exposure |
| `naive-host-ns` | `redis:7-alpine` | 34/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| `default-adguardhome` | `adguard/adguardhome:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-alloy` | `grafana/alloy:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-almalinux` | `almalinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-alpine` | `alpine:3.21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-amazoncorretto` | `amazoncorretto:21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-amazonlinux` | `amazonlinux:2023` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-arangodb` | `arangodb:3.12` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-archlinux` | `archlinux:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-authelia` | `authelia/authelia:4.38` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-blackbox` | `prom/blackbox-exporter:v0.25.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-bun` | `oven/bun:1.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-busybox` | `busybox:1.37` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-caddy` | `caddy:2-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cassandra` | `cassandra:5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-chroma` | `chromadb/chroma:0.5.23` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-chronograf` | `chronograf:1.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-clickhouse` | `clickhouse:24.8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-clojure` | `clojure:temurin-21-tools-deps` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cockroach` | `cockroachdb/cockroach:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-consul` | `hashicorp/consul:1.20` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-couchdb` | `couchdb:3.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dart` | `dart:3.6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-debian` | `debian:12-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-deno` | `denoland/deno:2.1.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-docker-dind` | `docker:27-dind` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dragonfly` | `docker.dragonflydb.io/dragonflydb/dragonfly:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-drone` | `drone/drone:2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-drupal` | `drupal:11-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-elixir` | `elixir:1.18-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-envoy` | `envoyproxy/envoy:v1.32-latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-erlang` | `erlang:27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-fedora` | `fedora:41` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-flink` | `flink:1.20` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-forgejo` | `codeberg.org/forgejo/forgejo:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gcc` | `gcc:14` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ghost` | `ghost:5-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gitea` | `gitea/gitea:1.22` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gogs` | `gogs/gogs:0.13` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-golang` | `golang:1.23-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gradle` | `gradle:8-jdk21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-haskell` | `haskell:9.8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-httpd` | `httpd:2.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-influxdb` | `influxdb:2.7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-jellyfin` | `jellyfin/jellyfin:10.10.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-joomla` | `joomla:5-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-julia` | `julia:1.11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-kapacitor` | `kapacitor:1.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-keydb` | `eqalpha/keydb:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-localstack` | `localstack/localstack:4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mailpit` | `axllent/mailpit:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mariadb` | `mariadb:11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-matomo` | `matomo:5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-maven` | `maven:3.9-eclipse-temurin-21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mediawiki` | `mediawiki:1.43` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-meilisearch` | `getmeili/meilisearch:v1.11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-metabase` | `metabase/metabase:v0.52.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-minio` | `minio/minio:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo` | `mongo:7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo-express` | `mongo-express:1.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mosquitto` | `eclipse-mosquitto:2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mysql` | `mysql:8.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nats` | `nats:2.10-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-neo4j` | `neo4j:5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-netdata` | `netdata/netdata:stable` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nextcloud` | `nextcloud:30-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx` | `nginx:1.27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-node` | `node:22-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nomad` | `hashicorp/nomad:1.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-openresty` | `openresty/openresty:alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-opensuse-leap` | `opensuse/leap:15.6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-oraclelinux` | `oraclelinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-perl` | `perl:5.40-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pgvector` | `pgvector/pgvector:pg17` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-photon` | `photon:5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-php` | `php:8.4-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-phpmyadmin` | `phpmyadmin:5.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pihole` | `pihole/pihole:2024.07.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgis` | `postgis/postgis:17-3.5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgres` | `postgres:17-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-promtail` | `grafana/promtail:3.3.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pypy` | `pypy:3.10-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-python` | `python:3.13-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-qdrant` | `qdrant/qdrant:v1.12.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-questdb` | `questdb/questdb:8.2.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rabbitmq` | `rabbitmq:4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis` | `redis:7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis-stack` | `redis/redis-stack-server:7.4.0-v3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redmine` | `redmine:6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-registry` | `registry:2.8.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rethinkdb` | `rethinkdb:2.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rockylinux` | `rockylinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ruby` | `ruby:3.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rust` | `rust:1.83-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-scylla` | `scylladb/scylla:6.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-snipe-it` | `snipe/snipe-it:v7.0.13` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-swagger-ui` | `swaggerapi/swagger-ui:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-telegraf` | `telegraf:1.33-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-temurin` | `eclipse-temurin:21-jre-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-terraform` | `hashicorp/terraform:1.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tomcat` | `tomcat:10.1-jre21-temurin` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-traefik` | `traefik:v3.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-typesense` | `typesense/typesense:27.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ubuntu` | `ubuntu:24.04` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-unit` | `unit:1.34.1-minimal` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-uptime-kuma` | `louislam/uptime-kuma:1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-valkey` | `valkey/valkey:8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-vault` | `hashicorp/vault:1.18` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-vaultwarden` | `vaultwarden/server:1.32.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-vector` | `timberio/vector:0.43.0-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-victoriametrics` | `victoriametrics/victoria-metrics:v1.107.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-weaviate` | `semitechnologies/weaviate:1.28.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-wordpress` | `wordpress:6-php8.3-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-zookeeper` | `zookeeper:3.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-adminer` | `adminer:4.8.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-alertmanager` | `prom/alertmanager:v0.28.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-cp-kafka` | `confluentinc/cp-kafka:7.8.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-emqx` | `emqx:5` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-fluentd` | `fluent/fluentd:v1.17` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-grafana` | `grafana/grafana:11.4.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-graylog` | `graylog/graylog:6.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-groovy` | `groovy:4` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-haproxy` | `haproxy:3.1-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-hydra` | `oryd/hydra:v2.2.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jaeger` | `jaegertracing/jaeger:2.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jenkins` | `jenkins/jenkins:lts` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jetty` | `jetty:12-jre21` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kafka` | `apache/kafka:3.9.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kong` | `kong:3.8` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-loki` | `grafana/loki:3.3.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-mailhog` | `mailhog/mailhog:v1.0.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-mattermost` | `mattermost/mattermost-team-edition:10.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-memcached` | `memcached:1.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-n8n` | `n8nio/n8n:1.71.3` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nexus3` | `sonatype/nexus3:3.75.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nginx-unpriv` | `nginxinc/nginx-unprivileged:1.27-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-node-exporter` | `prom/node-exporter:v1.8.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-opensearch` | `opensearchproject/opensearch:2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-percona` | `percona:8.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pgadmin` | `dpage/pgadmin4:8` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-prometheus` | `prom/prometheus:v3.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pulsar` | `apachepulsar/pulsar:3.3.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pushgateway` | `prom/pushgateway:v1.10.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-redisinsight` | `redis/redisinsight:latest` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-redpanda` | `redpandadata/redpanda:v24.2.11` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-solr` | `solr:9` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-sonarqube` | `sonarqube:community` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-tempo` | `grafana/tempo:2.6.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-tika` | `apache/tika:latest` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-varnish` | `varnish:7.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-verdaccio` | `verdaccio/verdaccio:6` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-wiki` | `requarks/wiki:2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `hardened-reference` | `nginx:1.27-alpine` | 100/100 | **A** | none |

**Grade distribution:** 1×A, 38×C, 115×D, 1×F.

Regenerate this file from a clean checkout with `examples/isolation-survey/survey.sh` (Docker required).
