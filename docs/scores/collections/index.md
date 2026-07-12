---
title: Container isolation scores by category
description: Ranked container isolation scores by category: databases, language runtimes, web servers, CI/CD, observability, and more. Graded 0-100 by ironctl scan.
---

# Container isolation scores by category

The [container isolation scores directory](../index.md) grades hundreds of the most-pulled public Docker images as they ship. These **14 category pages** rank them by topic, so you can compare like with like: a database against a database, a language runtime against a runtime. Every page is regenerated on the weekly survey refresh, so the rankings never go stale.

| Category | Images | Avg score | Best-isolated (default config) |
|----------|-------:|----------:|--------------------------------|
| [Vector databases](vector-databases.md) | 5 | 48/100 | [`chromadb/chroma:0.5.23`](../chroma.md) 48/100 🟠 D |
| [Search engines](search-engines.md) | 9 | 58/100 | [`elasticsearch:8.16.1`](../elasticsearch.md) 63/100 🟡 C |
| [Monitoring and observability](observability.md) | 27 | 56/100 | [`prom/alertmanager:v0.28.0`](../alertmanager.md) 63/100 🟡 C |
| [Message queues and streaming](message-queues.md) | 13 | 56/100 | [`apache/activemq-artemis:2.38.0`](../activemq-artemis.md) 63/100 🟡 C |
| [Databases](databases.md) | 38 | 51/100 | [`adminer:4.8.1`](../adminer.md) 63/100 🟡 C |
| [CI/CD and Git](ci-cd-git.md) | 19 | 54/100 | [`ghcr.io/actions/actions-runner:2.321.0`](../actions-runner.md) 63/100 🟡 C |
| [Language runtimes (Node.js, Python, Go, ...)](language-runtimes.md) | 33 | 48/100 | [`groovy:4`](../groovy.md) 63/100 🟡 C |
| [Web servers and proxies](web-servers.md) | 16 | 53/100 | [`haproxy:3.1-alpine`](../haproxy.md) 63/100 🟡 C |
| [Base OS images](base-os.md) | 12 | 48/100 | [`almalinux:9`](../almalinux.md) 48/100 🟠 D |
| [Identity and SSO](identity-sso.md) | 4 | 59/100 | [`dexidp/dex:v2.41.1`](../dex.md) 63/100 🟡 C |
| [Object storage](object-storage.md) | 4 | 48/100 | [`quay.io/ceph/ceph:v18.2.4`](../ceph.md) 48/100 🟠 D |
| [Data engineering and ML](data-ml.md) | 14 | 54/100 | [`apache/airflow:2.10.4`](../airflow.md) 63/100 🟡 C |
| [Infrastructure and networking](infra-networking.md) | 8 | 48/100 | [`hashicorp/consul:1.20`](../consul.md) 48/100 🟠 D |
| [Self-hosted apps](self-hosted-apps.md) | 38 | 51/100 | [`directus/directus:11.3.5`](../directus.md) 63/100 🟡 C |

Across these categories, **240 images** are ranked. Every grade comes from `ironctl scan`; run it on your own containers in ten seconds:

```bash
brew install ironsecco/ironclaw/ironclaw
ironctl scan my-container
```

- [All container isolation scores &rarr;](../index.md), every scorecard, worst-isolated first.
- [Container Isolation Leaderboard &rarr;](../leaderboard.md), the whole dataset ranked best to worst.
- [Scan any container &rarr;](../../scan.md), the full command reference.

---

*Generated from a reproducible survey by `examples/isolation-survey/gen_scorecards.py`. Categories are keyword classified from the survey, so new images join the right ranking automatically on each weekly refresh.*
