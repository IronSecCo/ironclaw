---
title: "Run untrusted LLM-generated code safely"
description: "How to execute LLM-generated code and agent tool-calls without trusting them. The containment pattern: isolate first, allowlist egress, keep secrets host-side, and gate every config change."
---

# Run untrusted LLM-generated code safely

An LLM writes a shell command, a Python snippet, or a tool-call, and something in your
stack runs it. That output was shaped by whatever went into the model's context: a
document, a web page, a prior tool result, any of which an attacker may control. So the
honest framing is not "is this code malicious?" but "**assume it is, and make it not
matter.**"

You cannot solve this by reviewing the code, because at agent speed nobody is reviewing
every command, and you cannot solve it by asking the model to behave, because a system
prompt is a request, not a boundary. The fix lives *below* the model, in the runtime
that executes its output.

## The containment pattern

Whether you build this yourself or use IronClaw, the shape is the same:

- **Isolate before you execute.** Run the code in a sandbox with no network namespace,
  a read-only root filesystem, dropped capabilities, and (on Linux) a second kernel
  under it. See [How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md) for the
  specific edges.
- **Make egress an allowlist, not a default.** Untrusted code that cannot open a socket
  cannot exfiltrate. Reach approved APIs only through a host-owned egress broker that
  enforces a deny-by-default destination allowlist, so a snippet that tries to POST your
  environment to `evil.example.com` simply has nowhere to send it.
- **Keep secrets out of the box.** The model API key, queue keys, and tokens live
  host-side. Inject credentials into the outbound call *outside* the sandbox so the key
  never enters the environment the untrusted code runs in. Then a leaked env dump leaks
  nothing.
- **Gate every capability change.** Untrusted code must not be able to widen its own
  permissions. New tools, new egress hosts, new mounts, all held for human approval.

## Do it with IronClaw

IronClaw runs every agent session, and therefore every piece of model-generated
tool-call, inside a sealed sandbox by default. You do not opt code into isolation; it is
the floor. Start the runtime and drive it entirely over its local API, no model key
required for the demo posture:

```bash
# Start the sealed control-plane
docker compose -f docker-compose.demo.yml up --build -d

# Send a message to an agent; its work runs inside the sandbox, not on your host
curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' \
  -H 'content-type: application/json' \
  -d '{"group":"dev-agent","text":"summarize the open issues"}'
```

Whatever the model decides to do in response executes behind `network=none`, a
read-only rootfs, and (on Linux) gVisor, and it cannot reach a secret or a network host
that an operator has not approved. That boundary is exercised on every push by a
red-team containment gate and you can reproduce the escape attempts yourself; see
[Breaking our own sandbox](../breaking-our-own-sandbox.md).

## Where to go next

- The isolation internals: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- Stopping the injection upstream: [Prevent AI agent prompt-injection escape](prevent-ai-agent-prompt-injection-escape.md).
- The full checklist: [AI agent security best practices](ai-agent-security-best-practices.md).
- Run it with your model: [model providers](../providers/index.md).
- How this compares to raw container-plus-LLM glue: [comparison](../comparison.md).
