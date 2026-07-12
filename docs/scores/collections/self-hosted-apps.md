---
title: "Most secure self-hosted app container images, ranked by isolation score"
description: "Ranked isolation scores for 38 self-hosted app container images, graded 0-100 by ironctl scan. Best 63/100, average 51/100. See which ship hardened."
---

# Most secure self-hosted app container images, ranked by isolation score

How isolated are the most-pulled **self-hosted app** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **38 self-hosted applications (Nextcloud, Jellyfin, Immich, WordPress, Ghost, Gitea alternatives)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 63/100** (average **51/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No self-hosted app image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`directus/directus:11.3.5`](../directus.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`mattermost/mattermost-team-edition:10.2`](../mattermost-team-edition.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`miniflux/miniflux:2.2.3`](../miniflux.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`outlinewiki/outline:0.82.0`](../outline.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`ghcr.io/plankanban/planka:1.24.2`](../planka.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`ghcr.io/umami-software/umami:postgresql-v2.14.0`](../umami.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`wekanteam/wekan:v7.72`](../wekan.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 8 | [`requarks/wiki:2`](../wiki.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 9 | [`actualbudget/actual-server:24.12.0`](../actual-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`adguard/adguardhome:latest`](../adguardhome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`appwrite/appwrite:1.6.0`](../appwrite.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`ghcr.io/advplyr/audiobookshelf:2.17.5`](../audiobookshelf.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`baserow/baserow:1.30.1`](../baserow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`lissy93/dashy:3.1.0`](../dashy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 15 | [`drupal:11-apache`](../drupal.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 16 | [`lscr.io/linuxserver/duplicati:2.1.0`](../duplicati.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 17 | [`freshrss/freshrss:1.24.3`](../freshrss.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 18 | [`ghost:5-alpine`](../ghost.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 19 | [`lscr.io/linuxserver/heimdall:2.6.3`](../heimdall.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 20 | [`ghcr.io/home-assistant/home-assistant:2024.12`](../home-assistant.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 21 | [`ghcr.io/gethomepage/homepage:v0.10.9`](../homepage.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 22 | [`ghcr.io/immich-app/immich-server:v1.123.0`](../immich-server.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 23 | [`jellyfin/jellyfin:10.10.3`](../jellyfin.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 24 | [`joomla:5-apache`](../joomla.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 25 | [`matomo:5`](../matomo.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 26 | [`mediawiki:1.43`](../mediawiki.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 27 | [`deluan/navidrome:0.54.3`](../navidrome.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 28 | [`nextcloud:30-apache`](../nextcloud.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 29 | [`nocodb/nocodb:0.257.2`](../nocodb.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 30 | [`binwiederhier/ntfy:v2.11.0`](../ntfy.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 31 | [`ghcr.io/paperless-ngx/paperless-ngx:2.14.7`](../paperless-ngx.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 32 | [`pihole/pihole:2024.07.0`](../pihole.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 33 | [`redmine:6`](../redmine.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 34 | [`snipe/snipe-it:v7.0.13`](../snipe-it.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 35 | [`matrixdotorg/synapse:v1.121.1`](../synapse.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 36 | [`lscr.io/linuxserver/syncthing:1.28.1`](../syncthing.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 37 | [`wallabag/wallabag:2.6.10`](../wallabag.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 38 | [`wordpress:6-php8.3-apache`](../wordpress.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own self-hosted app container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the self-hosted app images ranked above:

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
