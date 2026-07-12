---
title: "Most secure data engineering and ML container images, ranked by isolation score"
description: "Ranked isolation scores for 14 data engineering and ML container images, graded 0-100 by ironctl scan. Best 63/100, average 54/100. See which ship hardened."
---

# Most secure data engineering and ML container images, ranked by isolation score

How isolated are the most-pulled **data engineering and ML** container images when you `docker run` them with plain defaults, no hardening flags? This page ranks **14 data pipelines, analytics, and ML platforms (Airflow, Spark, Flink, MLflow, Jupyter)** on IronClaw's seven-dimension container containment scale (0-100), best-isolated first. Scores run **48/100 to 63/100** (average **54/100**). Higher is safer. Every score comes from `ironctl scan`, a credential-free audit you can run on your own containers in ten seconds.

> No data engineering and ML image ships fully isolated by default: the leaders still leave capabilities, egress, or a writable root filesystem open. The gap between any image here and a clean **100/100 grade A** is a handful of `docker run` flags, shown on every scorecard.

## Ranked best to worst

| Rank | Image | Score | Grade | Top gaps (default config) |
|-----:|-------|------:|:-----:|---------------------------|
| 🥇 1 | [`apache/airflow:2.10.4`](../airflow.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥈 2 | [`quay.io/jupyter/base-notebook:latest`](../base-notebook.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 🥉 3 | [`apache/nifi:2.0.0`](../nifi.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 4 | [`apache/spark:3.5.3`](../spark.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 5 | [`apache/superset:4.1.1`](../superset.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 6 | [`apache/zeppelin:0.11.2`](../zeppelin.md) | 63/100 | 🟡 **C** | Dropped capabilities, Network isolation / egress, Read-only root filesystem |
| 7 | [`flink:1.20`](../flink.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 8 | [`jupyterhub/jupyterhub:5.2.1`](../jupyterhub.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 9 | [`metabase/metabase:v0.52.4`](../metabase.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 10 | [`ghcr.io/mlflow/mlflow:v2.19.0`](../mlflow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 11 | [`ollama/ollama:0.5.4`](../ollama.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 12 | [`rocker/rstudio:4.4.2`](../rstudio.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 13 | [`storm:2.7.1`](../storm.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| 14 | [`tensorflow/tensorflow:2.18.0`](../tensorflow.md) | 48/100 | 🟠 **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |

## Scan your own data engineering and ML container

These grades come from `ironctl scan`, one credential-free command that audits any running container, docker-compose service, or Kubernetes manifest, not just the data engineering and ML images ranked above:

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
