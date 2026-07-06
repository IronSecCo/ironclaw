---
title: "Sandbox your Mastra agent with IronClaw"
description: How to sandbox a Mastra (TypeScript) agent. Run your Mastra agent's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your Mastra agent with IronClaw

You built an agent with **Mastra**, the TypeScript agent framework: a model, an
`Agent`, and tools created with `createTool`. That is a great way to design
*behavior*. The problem starts when it runs somewhere real: a Mastra tool's
`execute` runs **in your own Node process**, with your API key in memory and
unrestricted outbound network. One prompt-injected instruction and a shell tool is a
shell on your box.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card, the
model key held **host-side** and never in the agent, and every privileged tool call
routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command Mastra-to-IronClaw example lives at
    [`examples/integrations/mastra`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/mastra):
    a real Mastra tool (`createTool`) whose `execute` runs inside an IronClaw
    sandbox, driven over one benign task plus a battery of escape attempts, each
    reported blocked. Zero credentials, just Docker and Node.js 22+.

## Why sandbox this

A typical Mastra tool executes on the host:

```ts
import { createTool } from "@mastra/core/tools";
import { z } from "zod";
import { execSync } from "node:child_process";

export const shellTool = createTool({
  id: "shell",
  description: "Run a shell command",
  inputSchema: z.object({ command: z.string() }),
  execute: async ({ command }) => execSync(command).toString(), // runs on YOUR box
});
```

Three things are true of that tool, and all three are risks:

1. **The key is in the process.** Anything that can read `process.env` can read your
   provider key.
2. **`execute` runs with your privileges.** `execSync` runs on your host, with your
   filesystem and network.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction. The
[example](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/mastra)
keeps your agent exactly as designed and points the tool's execution at a sealed
per-session sandbox.

## See it work first (no credentials)

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
examples/integrations/mastra/run.sh
```

It brings up the offline `mock` provider (no model key), builds a real Mastra tool,
and drives its `execute` over one benign task plus a battery of escape attempts —
network exfil, host-filesystem read, Docker-socket takeover — each reported
**BLOCKED**. The run exits non-zero if any containment expectation fails, so it
doubles as a CI check.

## The fix, in your agent

Keep the agent; swap the tool. The example's `makeSandboxTool(sandbox)` returns an
ordinary Mastra tool, so it drops into `new Agent({ tools: { ... } })` with no other
change:

```ts
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

const sandbox = await new IronClawSandbox().engage();
const tool = makeSandboxTool(sandbox);   // execute() runs inside the sealed sandbox
// const agent = new Agent({ tools: { sandboxedShell: tool }, ... });
```

Every command the model routes through that tool now executes inside the box as an
unprivileged uid, with `network=none` and no host mounts.

## What you gained

- **The key left the agent.** It lives host-side and is injected per request; a
  compromised agent has nothing to steal.
- **`network=none` by default.** The sandbox has no NIC. The only egress is the
  audited model-proxy socket, plus whatever hosts you explicitly allowlist.
- **Privileged actions are gated.** Registering a tool, spawning another agent, or
  reaching a new host flows through a human-approval gateway and the
  [audit log](../architecture.md).

Same agent you designed in Mastra. A perimeter it never had.

## Next

- [Sandbox your Vercel AI SDK agent](vercel-ai-sdk.md)
- [Sandbox your LangChain.js agent](langchain-js.md)
- [Security and isolation](../security-isolation.md)
