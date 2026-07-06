---
title: "Sandbox your Agno agent with IronClaw"
description: How to sandbox an Agno (ex-Phidata) agent. Run your Agno agent's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your Agno agent with IronClaw

You built an agent with **Agno** (formerly Phidata): a model, a persona, and a
`Toolkit` of functions the model can call. That is a great way to design *behavior*.
The problem starts when it runs somewhere real: an Agno agent runs its tools **in
your own process**, with your API key in memory and unrestricted outbound network.
One prompt-injected instruction and a `ShellTools` call is a shell on your box.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card, the
model key held **host-side** and never in the agent, and every privileged tool call
routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command Agno-to-IronClaw example lives at
    [`examples/integrations/agno`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/agno):
    a real `agno.tools.Toolkit` whose `sandboxed_shell` tool executes inside an
    IronClaw sandbox, driven over one benign task plus a battery of escape attempts,
    each reported blocked. Zero credentials, just Docker.

## Why sandbox this

A typical Agno agent hands the model a host-executing toolkit:

```python
from agno.agent import Agent
from agno.models.openai import OpenAIChat
from agno.tools.shell import ShellTools

agent = Agent(
    model=OpenAIChat(id="gpt-4o", api_key="sk-..."),  # key lives in this process
    tools=[ShellTools()],                              # ShellTools runs on YOUR box
)
agent.print_response("summarize today's incidents")
```

Three things are true of that agent, and all three are risks:

1. **The key is in the process.** Anything that can read `os.environ` can read `sk-...`.
2. **Tools run with your privileges.** `ShellTools` executes on your host, with your
   filesystem and network.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction. The
[example](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/agno)
keeps your agent exactly as designed and swaps the toolkit for a sandboxed one.

## See it work first (no credentials)

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
examples/integrations/agno/run.sh
```

It brings up the offline `mock` provider (no model key), builds a real Agno
`Toolkit`, and drives its `sandboxed_shell` tool over one benign task plus a battery
of escape attempts — network exfil, host-filesystem read, Docker-socket takeover —
each reported **BLOCKED**. The run exits non-zero if any containment expectation
fails, so it doubles as a CI check.

## The fix, in your agent

Keep the agent; swap the toolkit. The example's `IronClawTools` is an ordinary
`agno.tools.Toolkit`, so it drops into `Agent(tools=[...])` with no other change:

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import IronClawTools
from agno.agent import Agent

with IronClawSandbox() as sandbox:
    agent = Agent(tools=[IronClawTools(sandbox)])   # sandboxed_shell runs inside the box
```

Agno turns the toolkit method's type hints and docstring into the LLM tool schema,
exactly as it does for any toolkit; the command just lands inside a sealed sandbox as
an unprivileged uid, with `network=none` and no host mounts.

## What you gained

- **The key left the agent.** It lives host-side and is injected per request; a
  compromised agent has nothing to steal.
- **`network=none` by default.** The sandbox has no NIC. The only egress is the
  audited model-proxy socket, plus whatever hosts you explicitly allowlist.
- **Privileged actions are gated.** Registering a tool, spawning another agent, or
  reaching a new host flows through a human-approval gateway and the
  [audit log](../architecture.md).

Same agent you designed in Agno. A perimeter it never had.

## Next

- [Sandbox your LangChain agent](langchain.md)
- [Choose your model provider](../providers/index.md)
- [Security and isolation](../security-isolation.md)
