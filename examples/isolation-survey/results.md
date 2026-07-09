# State of Container Isolation — survey results

Scanned **68 scenarios** with `ironctl scan` dev on 2026-07-09T17:37:22Z.

Each row is one popular public image run with a specific configuration, graded 0-100 across seven containment dimensions (non-root user, dropped capabilities, seccomp, network isolation, read-only rootfs, no docker.sock, no host namespaces). Higher is safer. See [README.md](./README.md) for the exact method and [images.txt](./images.txt) for the pinned manifest.

| Scenario | Image | Score | Grade | Top failed dimensions |
|----------|-------|------:|:-----:|-----------------------|
| `naive-privileged` | `python:3.13-alpine` | 19/100 | **F** | Dropped capabilities, Non-root user (uid != 0), Seccomp profile |
| `naive-ci-docker-sock` | `node:22-alpine` | 33/100 | **D** | Dropped capabilities, Non-root user (uid != 0), No docker.sock exposure |
| `naive-host-ns` | `redis:7-alpine` | 34/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| `default-alpine` | `alpine:3.21` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-amazonlinux` | `amazonlinux:2023` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-authelia` | `authelia/authelia:4.38` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-blackbox` | `prom/blackbox-exporter:v0.25.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-busybox` | `busybox:1.37` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-caddy` | `caddy:2-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-cassandra` | `cassandra:5.0` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-chronograf` | `chronograf:1.10` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-clickhouse` | `clickhouse:24.8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-consul` | `hashicorp/consul:1.20` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-couchdb` | `couchdb:3.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-debian` | `debian:12-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-drupal` | `drupal:11-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-fedora` | `fedora:41` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ghost` | `ghost:5-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-gitea` | `gitea/gitea:1.22` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-golang` | `golang:1.23-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-httpd` | `httpd:2.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-influxdb` | `influxdb:2.7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-joomla` | `joomla:5-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mariadb` | `mariadb:11` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo` | `mongo:7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mosquitto` | `eclipse-mosquitto:2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mysql` | `mysql:8.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nats` | `nats:2.10-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-neo4j` | `neo4j:5` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx` | `nginx:1.27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-node` | `node:22-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-openresty` | `openresty/openresty:alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-perl` | `perl:5.40-slim` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-php` | `php:8.4-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-phpmyadmin` | `phpmyadmin:5.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgres` | `postgres:17-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-python` | `python:3.13-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-questdb` | `questdb/questdb:8.2.1` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rabbitmq` | `rabbitmq:4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis` | `redis:7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-registry` | `registry:2.8.3` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rethinkdb` | `rethinkdb:2.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rockylinux` | `rockylinux:9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ruby` | `ruby:3.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rust` | `rust:1.83-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-telegraf` | `telegraf:1.33-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-temurin` | `eclipse-temurin:21-jre-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-tomcat` | `tomcat:10.1-jre21-temurin` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-traefik` | `traefik:v3.2` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-ubuntu` | `ubuntu:24.04` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-valkey` | `valkey/valkey:8` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-vault` | `hashicorp/vault:1.18` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-vaultwarden` | `vaultwarden/server:1.32.7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-wordpress` | `wordpress:6-php8.3-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-zookeeper` | `zookeeper:3.9` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-adminer` | `adminer:4.8.1` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-alertmanager` | `prom/alertmanager:v0.28.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-grafana` | `grafana/grafana:11.4.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-haproxy` | `haproxy:3.1-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jaeger` | `jaegertracing/jaeger:2.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-jetty` | `jetty:12-jre21` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-kong` | `kong:3.8` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-memcached` | `memcached:1.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-nginx-unpriv` | `nginxinc/nginx-unprivileged:1.27-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-node-exporter` | `prom/node-exporter:v1.8.2` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-prometheus` | `prom/prometheus:v3.1.0` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `default-varnish` | `varnish:7.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `hardened-reference` | `nginx:1.27-alpine` | 84/100 | **B** | Dropped capabilities |

**Grade distribution:** 1×B, 12×C, 54×D, 1×F.

Regenerate this file from a clean checkout with `examples/isolation-survey/survey.sh` (Docker required).
