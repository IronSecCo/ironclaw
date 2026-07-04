---
title: "IronClaw vs running AI agents unsandboxed"
description: "What a tool-enabled AI agent can actually do when it runs with no sandbox, why prompt injection makes that the default risk, and the controls that change the outcome."
---

# IronClaw vs running AI agents unsandboxed

The most common way to run an autonomous agent today is the least safe: an LLM SDK in
a normal process, with tools wired straight to the host, holding your API keys as
environment variables. It works on the happy path. The question is what happens on the
unhappy one.

## The risk is the default, not the edge case

An agent with tools can read files, call APIs, spend money, and send messages. The
moment it also reads an inbox, a web page, or a tool result, an **attacker** can put
instructions in that content and steer those same tools. Prompt injection is not a rare
failure for an autonomous agent; it is the normal operating condition. Run that agent
unsandboxed and a single injected instruction has your filesystem, your network, and
your credentials.

## What each setup actually stops

| Question | Unsandboxed agent | IronClaw |
| --- | --- | --- |
| Compromised agent reaches the host filesystem? | Yes, full user access | No. Read-only sealed rootfs, per-conversation sandbox |
| Agent can open arbitrary network connections? | Yes | No. Sandbox runs `network=none` |
| Provider API key exfiltratable by the agent? | Yes, it is in the process env | No. Key stays host-side, injected by a proxy over a Unix socket |
| Capability change (new tool, new egress) needs approval? | No, agent just acts | Yes. Held at a human-approval gateway with no bypass |
| Kernel-level second boundary if the process is popped? | No, shared host kernel | Yes, gVisor (`runsc`) user-space kernel on Linux |
| Isolation you can prove, not just assert? | You wrote it, you audit it | Red-team containment gate runs on every push |

## When running unsandboxed is fine

Be honest about your own threat model. Running an agent with no sandbox is a reasonable
choice when **all** of these hold: the agent has no tools that touch anything you care
about, it never ingests untrusted content, and it holds no credential worth stealing. A
read-only chat summarizer over public data is not the same risk as an agent with shell
access and your cloud keys.

## When to reach for IronClaw

The moment an agent has real tools **and** can see content you do not control, the
boundary stops being optional. IronClaw's design starts from "treat every agent as
already compromised, and make the boundary hold anyway," so the controls above are the
baseline, not an add-on you remember to configure later.

## See it hold

You do not have to take the table on trust. The zero-credential offline demo drives the
full chat to sandbox to reply path locally:

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

## Where to go next

- The controls in detail: [How to sandbox an AI agent](../learn/how-to-sandbox-an-ai-agent.md).
- Why filtering is not the boundary: [Prevent prompt-injection escape](../learn/prevent-ai-agent-prompt-injection-escape.md).
- The full alternative-by-alternative rundown: [Why IronClaw](../comparison.md).
- Run it with your model: [model providers](../providers/index.md).
