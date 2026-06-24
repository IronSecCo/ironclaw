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

<figure markdown="span">
  ![Zero-credential chat demo: one command starts the offline mock-agent control-plane with no API key; a chat message engages the agent, which launches a real per-session sandbox container; the reply flows back through the encrypted per-session queue.](assets/demo.svg){ width="800" }
  <figcaption><b>Zero credentials, one command.</b> The offline <code>mock-agent</code> runs the full chat → per-session sandbox → reply path with no API key — production seals each sandbox with gVisor and <code>network=none</code>. See the <a href="quickstart.md">Quickstart</a>.</figcaption>
</figure>

<div class="grid cards" markdown>

-   :material-rocket-launch: **[Quickstart](quickstart.md)**

    From a clean clone to submitting, approving, and auditing your first agent
    action — in about five minutes, on your machine.

-   :material-scale-balance: **[Why IronClaw / vs. alternatives](comparison.md)**

    Evaluating your options? How IronClaw compares to hosted agent platforms,
    raw container + LLM glue, and other self-hosted runtimes — honestly.

-   :material-school: **[Tutorials](tutorials/index.md)**

    Hands-on, copy-pasteable walkthroughs: your first sandboxed agent, connecting
    Slack, and writing a custom channel adapter.

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
| See how IronClaw compares to the alternatives | [Why IronClaw](comparison.md) |
| Run IronClaw locally | [Quickstart](quickstart.md) |
| Follow a hands-on walkthrough | [Tutorials](tutorials/index.md) |
| Understand the design | [IronClaw, Explained](ironclaw-explained.md) → [Architecture](architecture.md) |
| Evaluate the security posture | [Security & trust](security.md) → [Threat model](threat-model.md) |
| Wire an agent to Slack / Discord / … | [Channel adapters](channels.md) |
| Extend an agent with curated capabilities | [Skills](skills.md) |
| Drive the control-plane API | [API reference](reference/api.md) |
