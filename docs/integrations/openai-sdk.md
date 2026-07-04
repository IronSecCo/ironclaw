---
title: "Sandbox your OpenAI Agents SDK agent with IronClaw"
description: How to sandbox an OpenAI Agents SDK agent. Run your OpenAI agent's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the API key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your OpenAI Agents SDK agent with IronClaw

You built an agent with the **OpenAI Agents SDK** (or plain OpenAI function
calling): an agent, a model, and a set of tools the model can call. The loop is
familiar, and so is the exposure. The SDK client holds your API key in the process,
your tool functions run with your full local privileges, and nothing stops the
process from reaching any host on the internet. One tool-call the model was talked
into making, and that is your box.

IronClaw runs the same job behind a **sealed sandbox**: no network card, the API
key held **host-side** and never in the agent, and every privileged tool call
routed through a human-approval gateway and an audit log. IronClaw already speaks
the OpenAI wire format, so this is a short trip.

!!! example "Runnable example"
    A one-command OpenAI-Agents-to-IronClaw example lives at
    [`examples/integrations/openai-agents`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/openai-agents):
    an Agents SDK agent whose tool and code execution is backed by an IronClaw
    sandbox, with a blocked escape attempt printed at the end. It ships with the
    integration examples. The credential-free demo below runs the same sealed loop
    today.

## The three-line fix

Stop running the agent's tool functions in your own process. Declare the same agent
to IronClaw and it runs inside a sealed, network-free sandbox instead:

```sh
export OPENAI_API_KEY=sk-...   # host-side only; the sandbox never sees this key
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &
ironctl agent create --name "Housekeeper" --provider openai --model gpt-4o \
  --instructions "Tidy the workspace and report what you changed." \
  --tool list_dir --tool read_file --tool write_file --yes
```

No `run_shell` that is your shell, no key in the code. Built-in tools act only in
the agent's private workspace; anything stronger is a reviewed MCP tool. Full
mapping below.

!!! info "IronClaw does not run your Python in the sandbox, and that is the point"
    IronClaw's sandbox has **no interpreter and no in-sandbox install**. You do not
    *wrap* the OpenAI SDK process; you re-declare the same agent (persona, model,
    tools) as an IronClaw agent group, and IronClaw runs it inside the sealed
    runtime. A prompt injection cannot introduce code where there is no interpreter
    to run it. See [Skills](../skills.md) and [Security and isolation](../security-isolation.md).

## Why sandbox this

A typical OpenAI-SDK tool loop:

```python
from openai import OpenAI

client = OpenAI(api_key="sk-...")          # key lives in this process

def run_shell(cmd: str) -> str:            # runs on YOUR box, YOUR privileges
    return subprocess.run(cmd, shell=True, capture_output=True, text=True).stdout

resp = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "clean up the temp files"}],
    tools=[{"type": "function", "function": {"name": "run_shell", ...}}],
)
# your code then executes whatever run_shell the model asked for
```

The risks are structural, not stylistic:

1. **The key is in the process.** `sk-...` is one memory read away.
2. **Tool functions run with your privileges.** `run_shell` is your shell. The
   model decides the argument; a poisoned input decides the model.
3. **Egress is wide open.** The process can call anywhere.

IronClaw removes all three by construction.

## See it work first (no credentials)

Watch the sealed loop run with the offline `mock` provider, no key required:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from the openai sdk"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

The reply comes back through a real per-session sandbox and encrypted queues. Tear
down with `docker compose -f docker-compose.demo.yml down`. The self-checking
version is [`examples/hello-ironclaw`](https://github.com/IronSecCo/ironclaw/tree/main/examples/hello-ironclaw).

## Port your OpenAI-SDK agent

| OpenAI SDK | IronClaw | Notes |
|---|---|---|
| `OpenAI(api_key="sk-...")` | `--provider openai` + host `OPENAI_API_KEY` | The key is injected host-side by the model-proxy. It never enters the sandbox. |
| `model="gpt-4o"` | `--model gpt-4o` | Or switch backend entirely: `anthropic`, `gemini`, `local`. See [providers](../providers/index.md). |
| System message | `--identity` / `--soul` / `--instructions` | Who the agent is and how it operates. |
| `tools=[{function ...}]` | `--tool <name>` (built-in) or an MCP server | Built-ins: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Your own functions attach over [MCP](../mcp.md). |
| `chat.completions.create(...)` | a message to the agent group | Same round-trip, now through the sealed queue. |

The tool loop above becomes a declared agent, no key in the code:

```sh
export OPENAI_API_KEY=sk-...          # host-side only; the sandbox never sees it
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &

ironctl agent create \
  --name "Housekeeper" \
  --provider openai --model gpt-4o \
  --instructions "You tidy the workspace and report what you changed." \
  --tool list_dir --tool read_file --tool write_file --yes
```

Note what is missing: there is no `run_shell` that is your shell. IronClaw's
built-in tools act only inside the agent's own private workspace, and anything more
powerful is an [MCP tool](../mcp.md) you register through the approval gateway, once,
in the open.

## What you gained

- **The key left the agent.** Injected host-side per request; a compromised agent
  has nothing to exfiltrate.
- **`network=none` by default.** No NIC in the sandbox; egress is the audited
  model-proxy socket plus any host you explicitly allowlist.
- **Privileged actions are gated.** New tool, new agent, new egress host: each is a
  reviewed [change request](../mcp.md), not a silent capability, and each lands in
  the [audit log](../architecture.md).

Same agent you built with the OpenAI SDK. A perimeter it never had.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Security and isolation](../security-isolation.md)
