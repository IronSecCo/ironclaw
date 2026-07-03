---
title: "AI agent security best practices"
description: "A practical checklist for securing autonomous AI agents: assume compromise, seal the sandbox, allowlist egress, keep secrets host-side, gate config changes, and make every claim auditable."
---

# AI agent security best practices

Most agent-security advice stops at "validate inputs" and "use a good system prompt."
Neither survives contact with a real prompt-injection attack. The practices below start
from a harder assumption and work down to controls you can actually verify. They apply
to any agent stack; where IronClaw ships a control by default, it is called out.

## 1. Assume the agent is already compromised

Design backwards from "an attacker controls the model's actions" rather than "the model
is basically trustworthy." Every control below earns its place by holding *even when the
model is hostile*. If a mitigation only works when the model behaves, it is not a
security control, it is a hope.

## 2. Isolate before you execute

Run each agent session in its own sandbox with, at minimum: no network namespace
(`network=none`), a read-only root filesystem with `nosuid,nodev,noexec` mounts, dropped
capabilities, `no_new_privs`, and a non-root user namespace. On Linux, add a second
kernel (gVisor `runsc`) so a kernel bug is not automatically a host escape. Details:
[How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md) and
[gVisor vs containers](gvisor-vs-container-ai-isolation.md).

## 3. Make egress an allowlist, not a default

A compromised agent that cannot open a socket cannot exfiltrate. Route model and API
traffic through host-owned sockets and enforce a deny-by-default destination allowlist,
so reaching a new host is an operator decision, not the agent's.

## 4. Keep secrets out of the sandbox

The model API key, queue keys, and tokens should live host-side and be injected into
outbound calls *outside* the box. If the secret never enters the environment the agent
runs in, a leaked env dump leaks nothing.

## 5. Isolate session state

Give each session its own encrypted store (in IronClaw, per-session SQLite queues with
their own 256-bit key, read-only inbound and append-only outbound). One session then
physically cannot read another's data, and nothing sits in plaintext on disk.

## 6. Gate every configuration change behind a human

An agent that can grant itself a new tool, egress host, mount, or persona has escalated
without escaping the sandbox. Hold every capability change at a deterministic approval
gateway: the agent may *ask*, only a human may *grant*, and no change kind may skip the
floor. This also catches a trojaned skill that quietly requests
`egress: evil.example.com`, it is visible and rejected at review time, not discovered
post-breach.

## 7. Make skills data, not code

Extensions (skills) should declare the grants they need and never ship an executable
script or self-install. MCP servers should stay host-side and gateway-gated so they add
tool reach without adding a boundary the sandbox can cross. See [MCP servers](../mcp.md)
and [skills](../skills.md).

## 8. Make every claim auditable

A security property you cannot verify is a marketing claim. Publish a versioned threat
model, exercise the boundary in CI, and let anyone reproduce the escape attempts.
IronClaw ships a [threat model](../threat-model.md) versioned with the code, a red-team
containment gate that runs on every push, and a
[Breaking our own sandbox](../breaking-our-own-sandbox.md) writeup you can rerun.

## Check the floor in one command

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor                                  # read-only preflight

# The non-negotiable invariant: an agent cannot change its own config
./bin/ironctl change submit --kind persona --group dev-agent --by alice
./bin/ironctl change approve <change-id> --by alice   # only a human grants
```

## Where to go next

- The reasoning behind the runtime: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- Contain a successful injection: [Prevent AI agent prompt-injection escape](prevent-ai-agent-prompt-injection-escape.md).
- Run untrusted model output: [Run untrusted LLM-generated code safely](run-untrusted-llm-code-safely.md).
- Run it with your model: [model providers](../providers/index.md).
- How IronClaw compares: [comparison](../comparison.md).
