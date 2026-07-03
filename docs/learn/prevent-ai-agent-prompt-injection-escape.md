---
title: "Prevent AI agent prompt-injection escape"
description: "Why prompt-injection filtering fails and where the real boundary lives. Contain a compromised agent with isolation, host-side secrets, and a human approval gateway, not with better prompts."
---

# Prevent AI agent prompt-injection escape

Prompt injection is when text the model reads, an email, a web page, a tool result,
carries instructions the model then follows as if you had given them. For an autonomous
agent with tools, a successful injection does not just produce a bad answer; it produces
a bad **action**: reading `~/.ssh`, POSTing your environment variables somewhere, or
spending money. The agent did not "go rogue," it did exactly what the injected
instructions said, with exactly the privileges you handed it.

## Why you cannot filter your way out

The tempting fix is to detect the injection: scan inputs for "ignore previous
instructions," add a system prompt that says "never exfiltrate secrets," fine-tune for
refusals. These help at the margin, but none is a boundary:

- Injection payloads are open-ended natural language. Any classifier you build, an
  attacker rephrases around.
- A system prompt is a request the model can be argued out of, not a wall it cannot
  cross.
- The threat is not the words; it is the **capability**. If a compromised agent can
  reach the network and the network can reach your secrets, the injection had somewhere
  to go.

So the durable question is not "how do I stop the injection?" but "**when an injection
succeeds, what can the agent actually do?**" Design so the answer is "nothing that
matters."

## Contain the blast radius

The boundary has to live below the model, where its output cannot reach:

- **No network to escape through.** A sandbox with no network namespace
  (`network=none`) has no socket to open. The injected "exfiltrate this" command has
  nowhere to send. See [How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md).
- **No secret to steal.** The model key, queue keys, and tokens stay host-side and are
  injected into outbound calls outside the sandbox, so they never enter the box the
  agent runs in.
- **No self-escalation.** A compromised agent that can grant itself a new tool, egress
  host, or mount has escalated without ever leaving the sandbox. So configuration
  changes never pass through the model's hands: every one is held at a deterministic
  approval gateway for a human decision. The agent can *ask*; only a human can *grant*.

The agent can be fully hostile and still cannot read another session's data, reach an
unapproved host, obtain a host secret, or widen its own permissions. That is
containment, not detection, and it holds against payloads nobody has seen yet.

## See it hold with IronClaw

The fastest way to trust a boundary is to watch it refuse something. In IronClaw, an
agent proposing a configuration change gets **held at the gateway** until a human
approves, no matter what any injected prompt told it to do:

```bash
docker compose -f docker-compose.demo.yml up --build -d

# Whatever the agent was told, a config change is HELD, never auto-applied
./bin/ironctl change submit --kind persona --group dev-agent --by alice
./bin/ironctl change pending                          # shows it waiting for a human
./bin/ironctl change approve <change-id> --by alice   # only a human can grant
```

Run that and the core invariant stops being a paragraph and becomes a command you ran.

## Where to go next

- The isolation internals: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- Running model output safely: [Run untrusted LLM-generated code safely](run-untrusted-llm-code-safely.md).
- The full checklist: [AI agent security best practices](ai-agent-security-best-practices.md).
- The versioned adversary model: [threat model](../threat-model.md).
- How this compares to hosted platforms: [comparison](../comparison.md).
