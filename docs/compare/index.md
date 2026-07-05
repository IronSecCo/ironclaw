---
title: "Compare IronClaw: sandboxing options for AI agents"
description: "How IronClaw compares to running agents unsandboxed, raw Docker, gVisor alone, and hosted sandboxes like E2B. Honest tables, when-to-use guidance, and runnable examples."
---

# Compare IronClaw

Deciding where to run an autonomous AI agent is really a decision about one thing:
**what stops the agent when it turns hostile.** Prompt injection, a poisoned tool
result, or a compromised dependency can all steer an agent that has real tools. These
pages compare IronClaw against the options teams actually weigh, honestly and with the
security boundary front and center.

Every IronClaw claim on these pages links to shipped code, a versioned threat model,
or a runnable example. We describe the *architectural pattern* of each alternative and
do not assert specific capabilities of any named third-party project, because those
change and you should verify them against that project's own docs.

## Pick the comparison that matches your question

- [IronClaw vs running AI agents unsandboxed](ironclaw-vs-unsandboxed-ai-agents.md) -
  the core risk: what a tool-enabled agent can do with no boundary at all, and the
  smallest set of controls that changes the outcome.
- [IronClaw vs raw Docker for agent isolation](ironclaw-vs-docker-agent-sandbox.md) -
  why a plain container is a good first step but a shared-kernel boundary, and what you
  still have to build on top of it.
- [IronClaw vs gVisor alone](ironclaw-vs-gvisor-alone.md) - a user-space kernel is a
  strong wall, but a wall is not a runtime. What the rest of the agent lifecycle needs.
- [IronClaw vs hosted agent sandboxes (E2B and similar)](ironclaw-vs-e2b-hosted-sandboxes.md) -
  the self-hosted vs managed trade: who holds your keys and data, and when each side wins.
- [Sandbox containment benchmark](sandbox-containment-benchmark.md) - a reproducible
  head-to-head: one fixed escape-attempt suite run against raw Docker, hardened runc, and
  gVisor, with honest labels for where E2B and Daytona sit.

## The short version

IronClaw is a **security-first, self-hosted runtime for autonomous AI agents.** It
assumes the agent could be compromised and builds a boundary you can verify: a
gVisor-backed sandbox with `network=none`, a read-only rootfs, dropped capabilities, a
host-side proxy so provider keys never enter the box, and a human-approval gateway for
capability changes. If that threat model matches how you think about giving an LLM the
ability to act, IronClaw is built for you.

For the full evidence-backed rundown across all alternatives, see the main
[Why IronClaw comparison page](../comparison.md). To run it yourself, the
[quickstart](../quickstart.md) goes from a clean clone to a held approval in about
five minutes.
