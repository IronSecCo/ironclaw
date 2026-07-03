---
title: "AI agent security and sandboxing: a practical guide"
description: "How to run AI agents and untrusted LLM-generated code safely. Sandboxing, prompt-injection containment, and isolation best practices, with runnable IronClaw examples."
---

# AI agent security and sandboxing

Give an AI agent tools and you have given it a way to act on the world: read files,
call APIs, spend money, send messages. Give it an inbox, a web page, or a tool result
and you have given an **attacker** a way to steer those actions. Prompt injection is
not a corner case for an autonomous agent; it is the normal operating condition.

This guide is a practical, vendor-honest walk through the problem and the controls
that actually hold. Every claim links to shipped code or a versioned threat model, and
every page carries a runnable IronClaw snippet so you can watch the boundary work
instead of taking our word for it.

## Start here

- [How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md) - the concrete controls
  (network, filesystem, kernel, secrets) and how to turn them on.
- [Run untrusted LLM-generated code safely](run-untrusted-llm-code-safely.md) - the
  pattern for executing model output you did not write.
- [Prevent AI agent prompt-injection escape](prevent-ai-agent-prompt-injection-escape.md)
  - why filtering fails and where the real boundary lives.
- [AI agent security best practices](ai-agent-security-best-practices.md) - a checklist
  you can apply to any agent stack, ours or not.
- [gVisor vs containers for AI isolation](gvisor-vs-container-ai-isolation.md) - what a
  second kernel buys you, and what it does not.

## Why IronClaw

IronClaw is a self-hosted agent runtime built on one uncomfortable assumption: **treat
every agent as already compromised, and design the boundary so it holds anyway.** If
you are weighing options, the [comparison page](../comparison.md) is an evidence-backed
look at IronClaw against hosted platforms, raw container-plus-LLM glue, and other
self-hosted runtimes. To run it with your own model, see the
[model providers](../providers/index.md) guide, and the
[quickstart](../quickstart.md) takes you from a clean clone to watching a
configuration change get held at the approval gateway in about five minutes.

For the deep dive on the runtime itself, read
[Why we run AI agents in gVisor](../gvisor-deep-dive.md).
