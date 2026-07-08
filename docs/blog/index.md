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
