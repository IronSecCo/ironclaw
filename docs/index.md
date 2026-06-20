---
title: IronClaw
description: Security-first, self-hosted AI agents — isolation you can prove, not just claim.
---

# IronClaw

**Security-hardened, self-hosted AI agents — isolation you can prove, not just claim.**

IronClaw runs autonomous agents the way a security team would want them run: every
agent lives in a per-session **sandbox**, every capability change passes through a
deterministic **human-approval gateway**, and every action lands in an append-only
**audit log**. There is no path that bypasses the gateway.

<div class="grid cards" markdown>

-   :material-rocket-launch: **[Quickstart](quickstart.md)**

    From a clean clone to submitting, approving, and auditing your first agent
    action — in about five minutes, on your machine.

-   :material-shield-lock: **[Security & trust](security.md)**

    The trust story: the threat model, the sealed-runtime invariants, and how a
    user verifies what they install.

-   :material-sitemap: **[Architecture](architecture.md)**

    The control-plane / sandbox split, the frozen contract between them, and the
    encrypted queues they speak over.

-   :material-api: **[API reference](reference/api.md)**

    The control-plane HTTP API (OpenAPI 3.1) consumed by `ironctl` and the web
    console.

</div>

## What makes IronClaw different

- **Assume the agent is hostile.** The [threat model](threat-model.md) treats the
  agent inside the sandbox as potentially compromised — by prompt injection, a
  poisoned tool result, or a hostile model output — and designs the blast radius
  around that assumption.
- **Every mutation is gated.** Persona, enabled tools, packages, wiring,
  permissions, and mounts are *held* at the gateway until a human approves them.
  See the [Quickstart](quickstart.md) for a hands-on demonstration.
- **Verifiable supply chain.** Every release is checksummed, keyless-signed
  (cosign), and carries build-provenance attestations. See the
  [Release runbook](release-runbook.md) for how to verify a download.

## Where to go next

| If you want to… | Read |
| --- | --- |
| Run IronClaw locally | [Quickstart](quickstart.md) |
| Understand the design | [IronClaw, Explained](ironclaw-explained.md) → [Architecture](architecture.md) |
| Evaluate the security posture | [Security & trust](security.md) → [Threat model](threat-model.md) |
| Wire an agent to Slack / Discord / … | [Channel adapters](channels.md) |
| Extend an agent with curated capabilities | [Skills](skills.md) |
| Drive the control-plane API | [API reference](reference/api.md) |
