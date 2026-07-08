---
title: "IronClaw blog: evidence-backed writing on agent containment"
description: "Long-form, evidence-backed posts on securing autonomous AI agents. Every claim links to shipped code, a versioned threat model, or a re-runnable benchmark."
---

# IronClaw blog

Long-form writing on running autonomous AI agents safely. Every post links its claims to
shipped code, a versioned threat model, or a benchmark you can re-run yourself. No
adjectives standing in for numbers.

## Posts

- [We ran the same escape suite against Docker, gVisor, E2B, and Daytona](containment-benchmark-docker-gvisor-e2b-daytona.md) -
  a reproducible containment benchmark. One fixed escape-attempt suite, scored by
  observed behavior. Raw Docker blocked 2 of 5, hardened runc 4 of 5, gVisor 5 of 5,
  with honest labels for the hosted platforms.
- [Audit your sandbox in 10 seconds with ironctl scan](audit-your-sandbox-in-10-seconds.md) -
  one command grades any container, compose service, or Kubernetes pod on a 0 to 100
  containment scale. Fail-closed, works on your own setups. A wide-open container scores
  23 of 100; a hardened IronClaw sandbox scores 100.
