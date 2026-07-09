---
title: Container Isolation Scores
description: The default-configuration container isolation score for 54+ of the most-pulled public Docker images, graded 0-100 across seven containment dimensions by ironctl scan.
---

# Container Isolation Scores

How isolated is the container you just `docker run`? This directory grades **54 of the most-pulled public images** as they ship, run with plain `docker run <image>` defaults, no hardening flags, on IronClaw's seven-dimension containment scale (0-100). Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

**The headline:** the average default image scores **51/100**. Grade distribution: 11×C, 43×D. Almost nothing you pull is isolated out of the box, it runs as root, keeps the full capability set, and has a writable root filesystem. The good news: every gap on these pages closes with a handful of `docker run` flags.

> **Scan your own container:** `brew install ironsecco/ironclaw/ironclaw && ironctl scan my-container`. See [Scan any container](../scan.md).

## Every image, worst-isolated first

| Image | Score | Grade | Top gaps (default config) |
|-------|------:|:-----:|---------------------------|
| [`alpine:3.21`](alpine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`amazonlinux:2023`](amazonlinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`busybox:1.37`](busybox.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`caddy:2-alpine`](caddy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/consul:1.20`](consul.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`couchdb:3.4`](couchdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`debian:12-slim`](debian.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`drupal:11-apache`](drupal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`eclipse-mosquitto:2`](eclipse-mosquitto.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`eclipse-temurin:21-jre-alpine`](eclipse-temurin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`fedora:41`](fedora.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ghost:5-alpine`](ghost.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`gitea/gitea:1.22`](gitea.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`golang:1.23-alpine`](golang.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`httpd:2.4-alpine`](httpd.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`influxdb:2.7-alpine`](influxdb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`joomla:5-apache`](joomla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mariadb:11`](mariadb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mongo:7`](mongo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`mysql:8.4`](mysql.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`nats:2.10-alpine`](nats.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`nginx:1.27-alpine`](nginx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`node:22-alpine`](node.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`openresty/openresty:alpine`](openresty.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`perl:5.40-slim`](perl.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`php:8.4-apache`](php.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`phpmyadmin:5.2`](phpmyadmin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`postgres:17-alpine`](postgres.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`python:3.13-alpine`](python.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rabbitmq:4-alpine`](rabbitmq.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`redis:7-alpine`](redis.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`registry:2.8.3`](registry.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rockylinux:9`](rockylinux.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ruby:3.4-alpine`](ruby.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`rust:1.83-alpine`](rust.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`vaultwarden/server:1.32.7`](server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`telegraf:1.33-alpine`](telegraf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`tomcat:10.1-jre21-temurin`](tomcat.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`traefik:v3.2`](traefik.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`ubuntu:24.04`](ubuntu.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`hashicorp/vault:1.18`](vault.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`wordpress:6-php8.3-apache`](wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`zookeeper:3.9`](zookeeper.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| [`adminer:4.8.1`](adminer.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/alertmanager:v0.28.0`](alertmanager.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`grafana/grafana:11.4.0`](grafana.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`haproxy:3.1-alpine`](haproxy.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`jetty:12-jre21`](jetty.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`kong:3.8`](kong.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`memcached:1.6-alpine`](memcached.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`nginxinc/nginx-unprivileged:1.27-alpine`](nginx-unprivileged.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/node-exporter:v1.8.2`](node-exporter.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`prom/prometheus:v3.1.0`](prometheus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| [`varnish:7.6-alpine`](varnish.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |

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
