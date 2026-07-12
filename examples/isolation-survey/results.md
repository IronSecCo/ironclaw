# State of Container Isolation — survey results

Scanned **256 scenarios** with `ironctl scan` dev+ebc627a3fc2d on 2026-07-12T02:57:13Z.

Each row is one popular public image run with a specific configuration, graded 0-100 across seven containment dimensions (non-root user, dropped capabilities, seccomp, network isolation, read-only rootfs, no docker.sock, no host namespaces). Higher is safer. See [README.md](./README.md) for the exact method and [images.txt](./images.txt) for the pinned manifest.

| Scenario | Image | Score | Grade | Top failed dimensions |
|----------|-------|------:|:-----:|-----------------------|
| `naive-privileged` | `python:3.13-alpine` | 19/100 | **F** | Dropped capabilities, Non-root user (uid != 0), Seccomp profile |
| `naive-ci-docker-sock` | `node:22-alpine` | 33/100 | **D** | Dropped capabilities, Non-root user (uid != 0), No docker.sock exposure |
| `naive-host-ns` | `redis:7-alpine` | 34/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| `default-act-runner` | `gitea/act_runner:0.2.11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-actual-server` | `actualbudget/actual-server:24.12.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-adguardhome` | `adguard/adguardhome:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-aerospike` | `aerospike/aerospike-server:7.2.0.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-alloy` | `grafana/alloy:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-almalinux` | `almalinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-alpine` | `alpine:3.21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-amazoncorretto` | `amazoncorretto:21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-amazonlinux` | `amazonlinux:2023` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-anaconda3` | `continuumio/anaconda3:2024.10-1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-appwrite` | `appwrite/appwrite:1.6.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-arangodb` | `arangodb:3.12` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-archlinux` | `archlinux:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-aspnet` | `mcr.microsoft.com/dotnet/aspnet:9.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-audiobookshelf` | `ghcr.io/advplyr/audiobookshelf:2.17.5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-authelia` | `authelia/authelia:4.38` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-baserow` | `baserow/baserow:1.30.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-blackbox` | `prom/blackbox-exporter:v0.25.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-bun` | `oven/bun:1.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-busybox` | `busybox:1.37` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-caddy` | `caddy:2-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cadvisor` | `gcr.io/cadvisor/cadvisor:v0.52.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cassandra` | `cassandra:5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-centrifugo` | `centrifugo/centrifugo:v5.4.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ceph` | `quay.io/ceph/ceph:v18.2.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-chroma` | `chromadb/chroma:0.5.23` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-chronograf` | `chronograf:1.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-clickhouse` | `clickhouse:24.8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-clojure` | `clojure:temurin-21-tools-deps` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cockroach` | `cockroachdb/cockroach:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-concourse` | `concourse/concourse:7.12.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-consul` | `hashicorp/consul:1.20` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cortex` | `quay.io/cortexproject/cortex:v1.18.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-couchdb` | `couchdb:3.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-crystal` | `crystallang/crystal:1.14.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dart` | `dart:3.6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dashy` | `lissy93/dashy:3.1.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-debian` | `debian:12-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-deno` | `denoland/deno:2.1.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dgraph` | `dgraph/dgraph:v24.0.5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-docker-dind` | `docker:27-dind` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-dragonfly` | `docker.dragonflydb.io/dragonflydb/dragonfly:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-drone` | `drone/drone:2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-drupal` | `drupal:11-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-duplicati` | `lscr.io/linuxserver/duplicati:2.1.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-elixir` | `elixir:1.18-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-envoy` | `envoyproxy/envoy:v1.32-latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-erlang` | `erlang:27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-fedora` | `fedora:41` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-firebird` | `firebirdsql/firebird:5.0.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-flink` | `flink:1.20` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-forgejo` | `codeberg.org/forgejo/forgejo:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-freshrss` | `freshrss/freshrss:1.24.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gcc` | `gcc:14` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ghost` | `ghost:5-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gitea` | `gitea/gitea:1.22` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gitlab-ce` | `gitlab/gitlab-ce:17.7.0-ce.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gitlab-runner` | `gitlab/gitlab-runner:v17.7.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gogs` | `gogs/gogs:0.13` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-golang` | `golang:1.23-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-graalvm-community` | `ghcr.io/graalvm/graalvm-community:21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gradle` | `gradle:8-jdk21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-haskell` | `haskell:9.8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-hasura` | `hasura/graphql-engine:v2.44.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-heimdall` | `lscr.io/linuxserver/heimdall:2.6.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-home-assistant` | `ghcr.io/home-assistant/home-assistant:2024.12` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-homepage` | `ghcr.io/gethomepage/homepage:v0.10.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-httpd` | `httpd:2.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ibmjava` | `ibmjava:8-jre` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ignite` | `apacheignite/ignite:2.16.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-immich-server` | `ghcr.io/immich-app/immich-server:v1.123.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-influxdb` | `influxdb:2.7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-jellyfin` | `jellyfin/jellyfin:10.10.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-joomla` | `joomla:5-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-julia` | `julia:1.11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-jupyterhub` | `jupyterhub/jupyterhub:5.2.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-k3s` | `rancher/k3s:v1.31.4-k3s1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-kapacitor` | `kapacitor:1.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-keydb` | `eqalpha/keydb:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-localstack` | `localstack/localstack:4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-lua` | `nickblah/lua:5.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mailpit` | `axllent/mailpit:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-manticore` | `manticoresearch/manticore:6.3.6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mariadb` | `mariadb:11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-matomo` | `matomo:5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-maven` | `maven:3.9-eclipse-temurin-21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mediawiki` | `mediawiki:1.43` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-meilisearch` | `getmeili/meilisearch:v1.11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-metabase` | `metabase/metabase:v0.52.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-milvus` | `milvusdb/milvus:v2.5.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-minio` | `minio/minio:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mlflow` | `ghcr.io/mlflow/mlflow:v2.19.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo` | `mongo:7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo-express` | `mongo-express:1.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mono` | `mono:6.12` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mosquitto` | `eclipse-mosquitto:2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mysql` | `mysql:8.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nats` | `nats:2.10-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-navidrome` | `deluan/navidrome:0.54.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-neo4j` | `neo4j:5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-netdata` | `netdata/netdata:stable` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nextcloud` | `nextcloud:30-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx` | `nginx:1.27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx-proxy` | `nginxproxy/nginx-proxy:1.6.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx-proxy-manager` | `jc21/nginx-proxy-manager:2.12.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nim` | `nimlang/nim:2.2.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nocodb` | `nocodb/nocodb:0.257.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-node` | `node:22-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nomad` | `hashicorp/nomad:1.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nsq` | `nsqio/nsq:v1.3.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ntfy` | `binwiederhier/ntfy:v2.11.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ollama` | `ollama/ollama:0.5.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-openresty` | `openresty/openresty:alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-opensuse-leap` | `opensuse/leap:15.6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-oraclelinux` | `oraclelinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-orientdb` | `orientdb:3.2.35` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-packer` | `hashicorp/packer:1.11.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-paperless-ngx` | `ghcr.io/paperless-ngx/paperless-ngx:2.14.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-perl` | `perl:5.40-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pgvector` | `pgvector/pgvector:pg17` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-photon` | `photon:5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-php` | `php:8.4-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-phpmyadmin` | `phpmyadmin:5.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pihole` | `pihole/pihole:2024.07.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgis` | `postgis/postgis:17-3.5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgres` | `postgres:17-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-promtail` | `grafana/promtail:3.3.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pulumi` | `pulumi/pulumi:3.144.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-pypy` | `pypy:3.10-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-python` | `python:3.13-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-qdrant` | `qdrant/qdrant:v1.12.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-questdb` | `questdb/questdb:8.2.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rabbitmq` | `rabbitmq:4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rancher` | `rancher/rancher:v2.10.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rclone` | `rclone/rclone:1.68.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis` | `redis:7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis-stack` | `redis/redis-stack-server:7.4.0-v3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redmine` | `redmine:6` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-registry` | `registry:2.8.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rest-server` | `restic/rest-server:0.13.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rethinkdb` | `rethinkdb:2.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rockylinux` | `rockylinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rstudio` | `rocker/rstudio:4.4.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ruby` | `ruby:3.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rust` | `rust:1.83-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-scala-sbt` | `sbtscala/scala-sbt:eclipse-temurin-21.0.5_11_1.10.7_3.6.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-scylla` | `scylladb/scylla:6.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-seaweedfs` | `chrislusf/seaweedfs:3.80` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-snipe-it` | `snipe/snipe-it:v7.0.13` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-statsd` | `statsd/statsd:v0.10.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-storm` | `storm:2.7.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-swag` | `lscr.io/linuxserver/swag:4.1.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-swagger-ui` | `swaggerapi/swagger-ui:latest` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-swift` | `swift:6.0.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-synapse` | `matrixdotorg/synapse:v1.121.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-syncthing` | `lscr.io/linuxserver/syncthing:1.28.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tailscale` | `tailscale/tailscale:v1.78.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tarantool` | `tarantool/tarantool:3.3.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-telegraf` | `telegraf:1.33-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-temurin` | `eclipse-temurin:21-jre-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tensorflow` | `tensorflow/tensorflow:2.18.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-terraform` | `hashicorp/terraform:1.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tidb` | `pingcap/tidb:v8.5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-timescaledb` | `timescale/timescaledb:2.17.2-pg17` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tomcat` | `tomcat:10.1-jre21-temurin` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tomee` | `tomee:9.1.3-jre17` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
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
| `default-wallabag` | `wallabag/wallabag:2.6.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-weaviate` | `semitechnologies/weaviate:1.28.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-wireguard` | `lscr.io/linuxserver/wireguard:1.0.20210914` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-wordpress` | `wordpress:6-php8.3-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-yugabyte` | `yugabytedb/yugabyte:2.23.0.0-b710` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-zookeeper` | `zookeeper:3.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-zulu-openjdk` | `azul/zulu-openjdk:21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-actions-runner` | `ghcr.io/actions/actions-runner:2.321.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-activemq-artemis` | `apache/activemq-artemis:2.38.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-adminer` | `adminer:4.8.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-airflow` | `apache/airflow:2.10.4` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-alertmanager` | `prom/alertmanager:v0.28.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-argocd` | `quay.io/argoproj/argocd:v2.13.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-base-notebook` | `quay.io/jupyter/base-notebook:latest` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-cp-kafka` | `confluentinc/cp-kafka:7.8.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-dex` | `dexidp/dex:v2.41.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-directus` | `directus/directus:11.3.5` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-druid` | `apache/druid:31.0.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-elasticsearch` | `elasticsearch:8.16.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-emqx` | `emqx:5` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-fluentd` | `fluent/fluentd:v1.17` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-grafana` | `grafana/grafana:11.4.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-graylog` | `graylog/graylog:6.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-groovy` | `groovy:4` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-haproxy` | `haproxy:3.1-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-harbor-core` | `goharbor/harbor-core:v2.12.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-hazelcast` | `hazelcast/hazelcast:5.5.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-hydra` | `oryd/hydra:v2.2.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jaeger` | `jaegertracing/jaeger:2.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jenkins` | `jenkins/jenkins:lts` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jetty` | `jetty:12-jre21` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kafka` | `apache/kafka:3.9.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-keycloak` | `quay.io/keycloak/keycloak:26.0.7` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kibana` | `kibana:8.16.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kong` | `kong:3.8` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-logstash` | `logstash:8.16.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-loki` | `grafana/loki:3.3.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-mailhog` | `mailhog/mailhog:v1.0.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-mattermost` | `mattermost/mattermost-team-edition:10.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-memcached` | `memcached:1.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-miniflux` | `miniflux/miniflux:2.2.3` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-n8n` | `n8nio/n8n:1.71.3` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nexus3` | `sonatype/nexus3:3.75.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nginx-unpriv` | `nginxinc/nginx-unprivileged:1.27-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nifi` | `apache/nifi:2.0.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-node-exporter` | `prom/node-exporter:v1.8.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-opensearch` | `opensearchproject/opensearch:2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-opensearch-dashboards` | `opensearchproject/opensearch-dashboards:2.18.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-outline` | `outlinewiki/outline:0.82.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-payara` | `payara/server-full:6.2024.12` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-percona` | `percona:8.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pgadmin` | `dpage/pgadmin4:8` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-planka` | `ghcr.io/plankanban/planka:1.24.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-prometheus` | `prom/prometheus:v3.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pulsar` | `apachepulsar/pulsar:3.3.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-pushgateway` | `prom/pushgateway:v1.10.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-redisinsight` | `redis/redisinsight:latest` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-redpanda` | `redpandadata/redpanda:v24.2.11` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-solr` | `solr:9` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-sonarqube` | `sonarqube:community` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-spark` | `apache/spark:3.5.3` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-standalone-chrome` | `selenium/standalone-chrome:4.27.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-superset` | `apache/superset:4.1.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-teamcity-server` | `jetbrains/teamcity-server:2024.12` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-tempo` | `grafana/tempo:2.6.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-thanos` | `quay.io/thanos/thanos:v0.37.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-tika` | `apache/tika:latest` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-umami` | `ghcr.io/umami-software/umami:postgresql-v2.14.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-varnish` | `varnish:7.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-verdaccio` | `verdaccio/verdaccio:6` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-vernemq` | `vernemq/vernemq:2.0.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-vespa` | `vespaengine/vespa:8.453.24` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-wekan` | `wekanteam/wekan:v7.72` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-wiki` | `requarks/wiki:2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-zeppelin` | `apache/zeppelin:0.11.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-zipkin` | `openzipkin/zipkin:3.4.4` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `hardened-reference` | `nginx:1.27-alpine` | 100/100 | **A** | none |

**Grade distribution:** 1×A, 69×C, 185×D, 1×F.

Regenerate this file from a clean checkout with `examples/isolation-survey/survey.sh` (Docker required).
