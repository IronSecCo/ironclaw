---
title: "FAQ: self-hosted, sandboxed AI agents"
description: Straight answers on running self-hosted, sandboxed AI agents — gVisor isolation, the zero-credential demo, host-side API-key custody, and production readiness.
---

# FAQ

Short, straight answers to the questions people ask before (and right after) their
first run. Everything here maps to shipped capability — see the
[roadmap](roadmap.md) for the single source of truth on what's done versus planned.

## Is it really sandboxed?

Yes. In the supported production posture every agent session runs in its **own
sandbox** built on **gVisor (`runsc`)** — a user-space kernel that intercepts
syscalls instead of handing them to the host kernel — with **`network=none`**, so
the sandbox has no direct network at all. Its only egress is a host-brokered
model-proxy unix socket. A **Kata Containers** (VM-isolation) backend is also
available. This is *stronger* isolation than the Docker/host-access model the peer
projects ship.

Two honest caveats:

- The **5-minute demo** ([Quickstart](quickstart.md)) deliberately relaxes the seal:
  it runs the sandbox as a **runc** container (shared host kernel), mounts the
  Docker socket, and pins a well-known token — a laptop convenience, clearly labeled,
  **not** the production posture. The mandatory approval gateway and encrypted queues
  stay intact even in the demo.
- Isolation is verified against a published, STRIDE-based
  [threat model](threat-model.md) — we describe what's in scope and what isn't,
  rather than asking you to take "secure" on faith.

## Do I need credentials to try it?

**No.** The fastest path uses the offline **`mock` provider** — no model key, no
gVisor — and runs the full engage → sandbox → reply loop so you can see an agent
actually respond:

```sh
docker compose -f docker-compose.demo.yml up --build -d
# then chat at http://127.0.0.1:8787/ui/  (token: ironclaw-demo)
```

To talk to a **real model**, set one provider credential **host-side** (it never
enters a sandbox) and point an agent group at that provider. See the
[Quickstart](quickstart.md#a-working-chat-in-5-minutes-no-credentials).

## Which model providers are supported?

**Anthropic, OpenAI, OpenRouter, and Codex**, plus a generic gateway URL
(`IRONCLAW_MODEL_GATEWAY_URL`) — all reached through the host model-proxy, so keys
stay host-side and never enter a sandbox. The zero-credential `mock` provider is
built in for offline demos and tests.

## Which channels are supported?

Twelve delivery surfaces today: **Slack, Discord, Telegram, Microsoft Teams,
Signal, iMessage, Webhook, WhatsApp, Email/SMTP, Matrix, Google Chat**, plus the
in-product **web chat playground**. Each built-in adapter and the environment
variable it reads is listed in the [Channels](channels.md) reference; you can also
[write your own adapter](writing-a-channel-adapter.md).

## What's the license? Is it really open source?

IronClaw is **open source under the GNU AGPLv3** (`LICENSE`). A **commercial
dual-license** is available for organisations that can't meet the AGPL's network
copyleft terms. Contributions are accepted under a
[Contributor License Agreement](https://github.com/IronSecCo/ironclaw/blob/main/CLA.md)
that lets the project dual-license your work — the CLA Assistant bot asks you to
sign it on your first pull request.

## How do I contribute?

Contributions of every kind are welcome — bug reports, fixes, new channel adapters,
docs, and tests.

1. Read
   [`CONTRIBUTING.md`](https://github.com/IronSecCo/ironclaw/blob/main/CONTRIBUTING.md)
   and build with `CGO_ENABLED=1` (see [Building from source](building.md)).
2. Browse
   [good first issues](https://github.com/IronSecCo/ironclaw/contribute) to find a
   starting point.
3. Open a pull request — the **CLA Assistant** bot will ask you to sign the
   [CLA](https://github.com/IronSecCo/ironclaw/blob/main/CLA.md) on your first PR.

Questions and ideas are best raised in
[GitHub Discussions](https://github.com/IronSecCo/ironclaw/discussions).

## How is IronClaw different from openclaw / nanoclaw?

Same job — self-hosted AI agents wired into your channels — but built
**security-first**: provable gVisor isolation with `network=none`, a **mandatory
human-approval gateway** that no mutation can bypass, SQLCipher-encrypted
per-session queues, and **cosign-signed releases with an SBOM and build
provenance** (a supply-chain story neither peer has claimed). The full side-by-side
is in the [roadmap comparison](roadmap.md#how-ironclaw-compares).

## Something isn't working — where do I start?

Run **`ironctl doctor`** — a read-only preflight that reports pass/warn/fail for
runtime, reachability, credentials, channels, and more, each with a fix. Then see
**[Troubleshooting](troubleshooting.md)**, which maps every doctor check to a
remediation and covers the common first-run build/Docker/port/userns failures.
