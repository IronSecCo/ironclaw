---
title: "IronClaw blog: evidence-backed writing on agent containment"
description: "Long-form, evidence-backed posts on securing autonomous AI agents. Every claim links to shipped code, a versioned threat model, or a re-runnable benchmark."
---

# IronClaw blog

Long-form writing on running autonomous AI agents safely. Every post links its claims to
shipped code, a versioned threat model, or a benchmark you can re-run yourself. No
adjectives standing in for numbers.

## Posts

- [State of Container Isolation 2026: we graded 16 popular images and 15 scored a D or worse](state-of-container-isolation-2026.md) -
  a reproducible survey of 16 popular public images in their common run
  configurations, graded 0 to 100 with `ironctl scan`. Only one hit an A;
  thirteen landed on a D. The median default image scores 48 of 100, running as
  root with the full capability set and a writable root filesystem.
- [We ran the same escape suite against Docker, gVisor, E2B, and Daytona](containment-benchmark-docker-gvisor-e2b-daytona.md) -
  a reproducible containment benchmark. One fixed escape-attempt suite, scored by
  observed behavior. Raw Docker blocked 2 of 5, hardened runc 4 of 5, gVisor 5 of 5,
  with honest labels for the hosted platforms.
- [Audit your sandbox in 10 seconds with ironctl scan](audit-your-sandbox-in-10-seconds.md) -
  one command grades any container, compose service, or Kubernetes pod on a 0 to 100
  containment scale. Fail-closed, works on your own setups. A wide-open container scores
  23 of 100; a hardened IronClaw sandbox scores 100.
- [Add a live Sandbox Isolation Score badge to your repo](add-a-sandbox-isolation-score-badge-to-your-repo.md) -
  generate a shields.io endpoint JSON with `ironctl scan --badge-json`, commit it as a
  static file, and render a live 0 to 100 A-to-F containment grade in your README. No
  server, no scan on every badge hit.

## Hardening guides

Per-image, data-driven walkthroughs: the default isolation grade, the exact dimensions that
fail, and the precise `ironctl scan --fix` flags that close the gap, with before and after scores.

- [How to harden a Postgres container](harden-postgres-container-isolation.md) -
  `postgres:17-alpine` scores 48 of 100 (D) on defaults: root, full capabilities, writable
  rootfs. Four flags take it to 100 of 100 (A). Is it safe for untrusted workloads?
- [How to harden a Redis container](harden-redis-container-isolation.md) -
  `redis:7-alpine` scores 48 of 100 (D). A read-only rootfs and dropped capabilities take the
  classic `CONFIG SET dir` file-write RCE off the table before auth. To 100 of 100 (A).
- [How to run untrusted Node.js code safely](run-untrusted-nodejs-code-safely.md) -
  the container is the sandbox, not the `vm` module. `node:22-alpine` is 48 of 100 (D) by
  default; `network=none` plus five flags make it a 100 of 100 (A) boundary for untrusted JS.
- [How to harden an nginx container](harden-nginx-container-isolation.md) -
  `nginx:1.27-alpine` scores 48 of 100 (D). The honest hardened ceiling for an internet-facing
  proxy is 89 of 100 (B), because it must reach its upstreams. Here is exactly why.

## Comparisons

Head-to-head reads, each backed by the same scan data, for the questions people
actually search.

- [Alpine vs Debian vs Ubuntu: does your base image change container isolation?](alpine-vs-debian-vs-ubuntu-container-isolation.md) -
  we scored the seven most-pulled base OS images. Every one landed on the same 48
  of 100. Base image choice barely moves the isolation needle; runtime flags do.
- [Docker default vs hardened: the container isolation score gap, measured](docker-default-vs-hardened-container-isolation.md) -
  151 images averaged 52 of 100 on defaults and 100 of 100 hardened. The exact
  48-point jump, dimension by dimension, and the six flags that produce it.
- [gVisor vs runc: container isolation compared](gvisor-vs-runc-container-isolation-compared.md) -
  they score identically on a config scan yet block a different number of real
  escape attempts. When hardened runc is enough and when you need a user-space
  kernel.
