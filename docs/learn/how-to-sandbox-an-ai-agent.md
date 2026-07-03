---
title: "How to sandbox an AI agent"
description: "A practical guide to sandboxing an autonomous AI agent: no network, read-only filesystem, dropped capabilities, a second kernel, and no path to host secrets. With runnable IronClaw commands."
---

# How to sandbox an AI agent

An AI agent with tools is an execution engine that takes instructions from untrusted
input. The moment its inbox, a web page, or a tool result can carry an injected
instruction, "what the agent can do" becomes "what an attacker can do." Sandboxing is
how you make that a bounded problem instead of an open one.

This page is the concrete version: the specific edges you seal, why each one matters,
and how to turn the whole posture on with IronClaw.

## The five edges that matter

A useful agent sandbox seals a small number of deliberately boring boundaries. Skip
any one and the others leak around it.

1. **Network.** The strongest control on this list. If the sandbox has no network
   namespace at all (`network=none`), a compromised agent cannot exfiltrate data or
   phone home, because there is no socket to open. Reach the model and approved APIs
   through host-owned sockets instead, so every byte that leaves crosses a choke point
   you control.
2. **Filesystem.** A read-only root filesystem with `nosuid,nodev,noexec` mounts means
   the agent cannot rewrite its own runtime or drop a persistent implant. Ship a
   compiled binary with no interpreter and no source in the box, and there is nothing
   to modify.
3. **Privileges.** Drop Linux capabilities, set `no_new_privs`, and run in a non-root
   user namespace so a process that gets code execution still cannot escalate.
4. **Kernel.** A normal container shares the host kernel, so one kernel bug is a host
   escape. Interposing a user-space kernel (gVisor `runsc`) shrinks the host syscall
   surface the agent can touch. See [gVisor vs containers](gvisor-vs-container-ai-isolation.md).
5. **Secrets and configuration.** The agent should never hold the model API key (inject
   it host-side, outside the box) and should never be able to change its own
   configuration to grant itself new reach. Route every capability change through a
   human approval gateway.

For the full trust-boundary walkthrough with a STRIDE pass per edge, see
[Why we run AI agents in gVisor](../gvisor-deep-dive.md) and the
[threat model](../threat-model.md).

## Turn it on with IronClaw

In IronClaw every agent session runs in its own sealed sandbox by default; you do not
assemble the controls above by hand. The production `docker compose up` is the hardened
posture (network-namespaceless sandbox, read-only rootfs, gVisor on Linux, host-side
secret injection). The demo control-plane starts the same machinery locally:

```bash
# Start the control-plane (demo posture, no model key required)
docker compose -f docker-compose.demo.yml up --build -d

# Point the admin CLI at it and diagnose the setup
./bin/ironctl doctor
```

The single most important property to verify is that **the agent cannot change its own
configuration.** Every capability change is held at a gateway for a human decision:

```bash
# An agent (or you) proposes a change; it is HELD, not applied
./bin/ironctl change submit --kind persona --group dev-agent --by alice

# A human reviews and approves it, only now does it take effect
./bin/ironctl change pending
./bin/ironctl change approve <change-id> --by alice
```

That deny-by-default floor is the difference between "we asked the model nicely" and "a
compromised agent still cannot grant itself new reach."

## Where to go next

- Running model output you did not write? See
  [Run untrusted LLM-generated code safely](run-untrusted-llm-code-safely.md).
- Worried about the injection itself? See
  [Prevent AI agent prompt-injection escape](prevent-ai-agent-prompt-injection-escape.md).
- Want the full checklist? See
  [AI agent security best practices](ai-agent-security-best-practices.md).
- Picking a model backend? See [model providers](../providers/index.md).
- Weighing IronClaw against alternatives? See the [comparison](../comparison.md).
