---
title: Why IronClaw / vs. the alternatives
description: An honest, evidence-backed comparison of IronClaw against hosted agent platforms, raw container + LLM glue, and other self-hosted agent runtimes.
---

# Why IronClaw — and how it compares

If you're evaluating where to run autonomous AI agents, you have real choices. This
page lays out, honestly, where IronClaw fits, where it doesn't, and how it differs
from the obvious alternatives — so you can decide quickly whether it's the right tool
for *your* job.

**The one-line positioning:** IronClaw is a **security-first, self-hosted runtime for
autonomous AI agents** — it assumes the agent itself could be compromised and builds a
boundary you can *verify*, not just trust. If that threat model matches how you think
about giving an LLM the ability to read, write, and act on your data, IronClaw is built
for you. If it doesn't, one of the alternatives below will serve you better.

!!! note "How to read this page"
    Every IronClaw claim here links to shipped code or docs you can check yourself.
    For the alternative *categories*, we describe the common architectural pattern of
    each — we deliberately **do not assert specific capabilities of any named third-party
    project**, because those change and you should verify them against that project's own
    documentation. The goal is to help you evaluate, not to win an argument.

## The competitive alternatives

When teams reach IronClaw, they're usually weighing it against one of four options:

| Alternative | What it looks like | Who it suits |
| --- | --- | --- |
| **Hosted agent platforms** (SaaS) | You sign up, paste an API key, and the vendor runs the agent and holds your data and credentials. | Teams who want zero ops and are comfortable with a third party holding keys and conversation data. |
| **Raw container + LLM glue** (DIY) | You wire an LLM SDK to a Docker container and a few tools yourself. | Builders who want full control and are willing to design and maintain the security boundary themselves. |
| **Other self-hosted agent runtimes** | An open-source agent framework you run on your own box. | Self-hosters who want to own the stack but vary widely in how much isolation they ship by default. |
| **Do nothing / build later** | Stay with a chat UI; no autonomous actions. | Anyone for whom the risk of an acting agent isn't yet worth the controls. |

IronClaw competes most directly in the **self-hosted** lane — it is an
AGPLv3 + commercial, security-hardened option for people who have already decided they
want to run agents on infrastructure they control.

## The axes a self-hoster actually evaluates

These are the questions that decide a self-hosting evaluation — and where IronClaw's
design choices show up.

### 1. Isolation model — *what stops a compromised agent?*

This is IronClaw's wedge. The production sandbox is **gVisor (`runsc`)**, a user-space
kernel that intercepts the agent's Linux syscalls and is the layer that actually
*enforces* `network=none`, a seccomp syscall allowlist, dropped Linux capabilities, and
a read-only rootfs.[^iso] The agent ships as a compiled Go binary with no source inside
the box to rewrite, and it runs one sandbox **per conversation**.

- **vs. hosted platforms:** isolation is the vendor's internal detail — you can't inspect
  or prove it.
- **vs. raw container + LLM glue:** a plain `docker run` shares the host kernel; a
  container escape is a host compromise. gVisor adds a second, syscall-level boundary that
  container-only setups don't have by default.
- **vs. other self-hosted runtimes:** many isolate with containers only, or run tools
  in-process. Whether a given project ships a kernel-level sandbox by default is the first
  thing to check — IronClaw treats it as the baseline, not an add-on.

!!! warning "Be honest with yourself about the platform"
    gVisor is **Linux-only**. On macOS/Windows, IronClaw's host side runs natively but a
    real agent sandbox falls back to runc inside Docker Desktop's Linux VM — a weaker,
    kernel-shared boundary, with `network=none` and the seccomp profile **not** auto-enforced.
    For anything past local development, run the sandbox host on Linux with gVisor.[^platform]

### 2. Credential handling & egress control — *where do your keys live?*

In IronClaw, the **model provider key never enters the sandbox.** The agent calls the
model over a Unix socket to a host-side proxy that injects the key and makes the outbound
HTTPS call; the sandbox itself runs with no network of its own.[^creds]

- **vs. hosted platforms:** the vendor holds your provider keys and your conversation
  data. That's the trade for zero ops.
- **vs. raw glue / other runtimes:** the common pattern is to pass the API key into the
  agent process as an environment variable — which means a prompt-injected or compromised
  agent can read and exfiltrate it. IronClaw's host-side injection is designed precisely to
  remove that path.

You can also remove the cloud key **entirely**: point IronClaw at a self-hosted,
OpenAI-compatible model — **Ollama, LM Studio, vLLM, or llama.cpp** — and the whole stack
runs on your own hardware with **no cloud credential at all** and no model data leaving the
box.[^local] Same isolation posture; the proxy just forwards to your loopback instead of an
upstream API.

### 3. Capability changes — *can the agent change what it's allowed to do?*

No. Persona, enabled tools, packages, channel wiring, permissions, and mounts are all
**held at a deterministic approval gateway** until a human approves them. The agent can
*request* a capability change but can never apply one — there is no path that bypasses the
gateway.[^gateway] This is the control that converts "the agent went rogue" from a breach
into a pending approval a human declines.

Most alternatives — hosted or DIY — let the agent (or its operator config) change its own
toolset at runtime. IronClaw makes that a gated, audited, human-in-the-loop event by design.

### 4. Multi-channel breadth — *how do users reach the agent?*

IronClaw ships **12 channel adapters** — Slack, Discord, Telegram, Microsoft Teams,
Signal, iMessage, WhatsApp, Email/SMTP, Matrix, Google Chat, and a generic Webhook — so
you talk to agents through the apps you already use.[^channels] Breadth varies widely
across alternatives; many self-hosted runtimes start web-UI-only.

### 5. Time-to-first-value — *how fast from "found the repo" to "running"?*

IronClaw has a **zero-credential local demo**: one command starts an offline `mock-agent`
control-plane with **no API key**, and a chat message drives the full chat → per-session
sandbox → reply path so you can see the architecture work before you commit a credential.[^demo]
You can have a capability change waiting at the gateway in under two minutes from a cold
machine.[^quickstart]

- **vs. hosted platforms:** sign-up is fast, but you've handed over a key and your data to
  evaluate.
- **vs. raw glue:** there's nothing to evaluate until you've built it.

### 6. Supply-chain posture — *can you verify what you're installing?*

For a security tool, this is non-negotiable. Every IronClaw release is checksummed,
keyless-signed with **cosign**, and carries **SLSA build-provenance** attestations and an
**SBOM** (SPDX + CycloneDX). The project publishes an **OpenSSF Scorecard** and is
registered for **OpenSSF Best Practices**, with dependencies SHA-pinned and CodeQL +
Scorecard running in CI.[^supply] The [release runbook](release-runbook.md) shows how to
verify a download yourself.

This is the kind of evidence a hosted platform can't hand you and a DIY stack rarely
builds — and it's exactly what a security-conscious evaluator looks for.

### 7. Self-host vs. hosted-SaaS trade-off — *what are you actually buying?*

| | Self-hosted (IronClaw) | Hosted SaaS |
| --- | --- | --- |
| Data & credential residency | Yours — on your infra | Vendor's |
| Isolation you can inspect | ✅ gVisor, open source, auditable | ❌ vendor-internal |
| Ops burden | You run it (Linux + gVisor for production) | None |
| Cost model | Your infra + your model spend | Subscription + usage |
| Lock-in | AGPLv3 + commercial; your stack | Vendor platform |
| Air-gapped / private network | ✅ control-plane is mesh-only (Tailscale), no public port | ❌ |

If "the vendor holds our keys and data" is a non-starter — common in security, finance,
healthcare, and regulated self-hosting — the self-hosted lane is the only one that fits,
and IronClaw competes there on a verifiable boundary.

## Where IronClaw is the right fit

Use IronClaw if you:

- want autonomous agents but **assume the agent could be compromised** and want the blast
  radius designed around that;
- need **data and credentials to stay on infrastructure you control** (regulated, private,
  or air-gapped environments);
- value a **verifiable supply chain** (signatures, SBOM, provenance) over a vendor's word;
- want **CLI-first / API-first** operation that's scriptable, auditable, and has no public
  web surface to phish or misconfigure.[^cli]

## Where IronClaw is *not* the right fit (yet)

We'd rather you find this out here than after an install:

- **You want zero ops.** IronClaw is self-hosted; production needs a Linux host with gVisor.
  A hosted platform will be less work.
- **You're on macOS/Windows for production.** The full isolation story is Linux-only; other
  platforms fall back to a weaker boundary.[^platform]
- **You need a polished end-user GUI.** IronClaw is CLI-first; the web console is a private,
  mesh-only admin surface, not a consumer product.
- **You need a feature still on the roadmap.** This page describes only **shipped**
  capability — anything experimental or planned lives in the [roadmap](roadmap.md), and the
  [project status](https://github.com/IronSecCo/ironclaw#project-status) is honest that
  IronClaw is **alpha**.

## Next steps

- **See it run with no credentials:** [Quickstart](quickstart.md)
- **Judge the security design:** [Security posture](security.md) → [Threat model](threat-model.md)
- **Understand the architecture:** [IronClaw, explained](ironclaw-explained.md) → [Architecture](architecture.md)
- **Verify a release yourself:** [Release runbook](release-runbook.md)

---

[^iso]: Isolation model — gVisor/`runsc` syscall interception, seccomp allowlist, dropped capabilities, read-only rootfs, `network=none`, and the sealed compiled-binary runtime. See the [Security posture](security.md), the [Threat model](threat-model.md), and the [Platform support](https://github.com/IronSecCo/ironclaw#platform-support) table.
[^platform]: Platform support and the macOS/Windows fallback to runc inside Docker Desktop's Linux VM. See [Platform support](https://github.com/IronSecCo/ironclaw#platform-support).
[^creds]: Host-side model-proxy key injection; the sandbox has no network and reaches the model only through a host proxy over a Unix socket. See the [Architecture overview](architecture.md) and the data-flow diagram in the [README](https://github.com/IronSecCo/ironclaw#how-it-works).
[^local]: First-class local / self-hosted model support via any OpenAI-compatible endpoint (Ollama, LM Studio, vLLM, llama.cpp). See [Run IronClaw with a 100% local model (Ollama)](tutorials/local-model-ollama.md).
[^gateway]: Deterministic human-approval gateway; every capability change is held until approved, with no bypass path. See the [Security posture](security.md) and [Quickstart](quickstart.md).
[^channels]: The 12 shipped channel adapters and how to wire them: [Channels](channels.md).
[^demo]: Zero-credential offline `mock-agent` demo driving the full chat → sandbox → reply path. See the [Quickstart](quickstart.md) and [Examples](examples.md).
[^quickstart]: Get a capability change waiting at the gateway in under two minutes from a cold machine. See the [Quickstart](quickstart.md).
[^supply]: Supply-chain posture — cosign keyless signatures, SLSA build provenance, SPDX + CycloneDX SBOMs, OpenSSF Scorecard, OpenSSF Best Practices, SHA-pinned dependencies, CodeQL. See the [Release runbook](release-runbook.md) and the badges in the [README](https://github.com/IronSecCo/ironclaw#readme).
[^cli]: CLI-first / API-first design; the control-plane API is mesh-only (Tailscale) with no public web port. See the [README](https://github.com/IronSecCo/ironclaw#cli-first-and-api-first) and [API reference](reference/api.md).
