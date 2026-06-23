---
title: Your first sandboxed agent in 5 minutes
description: From git clone to a running, replying agent — no credentials, no model key, one command.
---

# Your first sandboxed agent in 5 minutes

This tutorial takes you from `git clone` to a **running agent that actually replies** — with
**no API key**, **no model credential**, and **one command**. It uses IronClaw's offline
`mock-agent`, which runs the full **chat → per-session sandbox → reply** path so you can see the
machinery work before you wire up a real model.

By the end you'll have:

- A control-plane running locally in Docker.
- A real per-conversation **sandbox container** launched on demand.
- A round-trip reply that came back through IronClaw's **encrypted per-session queues**.

!!! info "What this is and isn't"
    This is the fastest way to *see IronClaw work*. It is a **local demo posture**, not the
    sealed production one — see [the security note](#what-the-demo-relaxes) at the end. When you're
    ready for the hardened path (gVisor + `network=none`), follow the
    [Quickstart's "first approved action"](../quickstart.md#your-first-approved-action).

## Prerequisites

- **Docker** — Docker Desktop on macOS/Windows is fine; Docker Engine on Linux.
- **A clone of the repo** (the command below does it for you).

That's it. No Go toolchain, no model key, no cloud account.

## 1. Clone and build the sandbox image

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw

bash container/build.sh    # builds the sandbox image once (~1–2 min)
```

`container/build.sh` builds the OCI image that each agent session runs inside. You only do this
once; subsequent runs reuse the image.

## 2. Start the demo control-plane

```sh
docker compose -f docker-compose.demo.yml up --build -d
```

This boots the control-plane wired to the offline `mock-agent` — no model key required. Give it a
few seconds to come up. It serves on `http://127.0.0.1:8787`.

## 3. Talk to the agent

You have two ways in. **Either** is a complete round trip.

### Option A — the browser console

```sh
open http://127.0.0.1:8787/ui/    # Linux: xdg-open http://127.0.0.1:8787/ui/
```

Open the **Chat** tab, pick **"Mock Agent (offline)"**, and say hi. If it asks for a token, paste
the demo token: `ironclaw-demo`.

### Option B — straight from the terminal

The demo pins a fixed loopback token, `ironclaw-demo`, so you can drive it with `curl`:

```sh
curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from my first agent"}'

sleep 3

curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages   # read the agent's reply
```

You'll see `mock-agent received: …` echoed back. That reply is proof that:

1. Your message engaged the agent group.
2. A **real sandbox container** launched for the conversation.
3. The reply flowed **back through the encrypted per-session queue** to the chat surface.

## 4. Tear it down

```sh
docker compose -f docker-compose.demo.yml down
```

## What the demo relaxes

The point of the demo is to remove every barrier to a first run, so it deliberately loosens the
production seal. The demo compose file:

- runs the control-plane as **root** and mounts the host **Docker socket**;
- launches the sandbox with **runc (shared host kernel), not gVisor**;
- pins a **well-known API token** (`ironclaw-demo`).

What it **does not** relax: the mandatory **approval gateway**, the **encrypted per-session
queues**, and **host-side model-credential custody** are all unchanged. Only the sandbox seal and
the token are relaxed — and only for a local demo.

!!! warning
    Don't run the demo compose file outside a local machine. The default `docker compose up` (the
    production `docker-compose.yml`) is the hardened posture: gVisor-isolated, `network=none`
    sandboxes that reach the model only through the host proxy.

## Next steps

- **Exercise the security model.** The
  [Quickstart's "first approved action"](../quickstart.md#your-first-approved-action) walks you
  through submitting a change and approving it at the human-approval gateway — IronClaw's core
  invariant.
- **Plug in a real chat app.** [Connect IronClaw to Slack](connect-slack.md) wires an agent group
  to a live Slack channel.
- **Chat with a real model.** Set a provider key host-side (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
  `OPENROUTER_API_KEY`, …) and point an agent group at that provider — see the
  [Quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials).
- **Understand the design.** [IronClaw, Explained](../ironclaw-explained.md) ·
  [Architecture](../architecture.md) · [Threat model](../threat-model.md).
