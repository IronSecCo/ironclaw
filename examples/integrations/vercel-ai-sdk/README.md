# Vercel AI SDK agents, sandboxed by IronClaw

Your [Vercel AI SDK](https://ai-sdk.dev/) agent runs untrusted, model-generated
code. Point that execution at an **IronClaw sandbox** instead of your host and the
same agent gets real code execution with **no network, no host filesystem, and no
Docker socket** — the isolation boundary IronClaw
[proves holds](../../red-team-escape/), not just promises.

```ts
import { generateText, stepCountIs } from "ai";
import { openai } from "@ai-sdk/openai";
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

const sandbox = await new IronClawSandbox().engage();     // engage a per-session sandbox
const sandboxedShell = makeSandboxTool(sandbox);          // a real AI SDK tool()

await generateText({
  model: openai("gpt-4o-mini"),
  tools: { sandboxedShell },                              // every command runs INSIDE the sandbox
  stopWhen: stepCountIs(5),
  prompt: "Run `id` and tell me which user the sandbox runs as.",
});
```

## Try it in one command

Zero credentials — the LLM side uses the offline **mock provider**, the sandbox
is real:

```sh
examples/integrations/vercel-ai-sdk/run.sh
```

It engages a live IronClaw per-session sandbox, drives the `sandboxedShell` tool
exactly as the AI SDK's agent loop would (`tool.execute({ command }, options)`),
runs one benign task plus a battery of escape attempts, and prints a PASS/FAIL
containment table:

```
  [OK ] benign task: run agent code                    ->  [exit 0] hello from inside the IronClaw sandbox uid=65532...
  [OK ] network egress: only loopback exists           ->  [exit 0] lo
  [OK ] network egress: DNS lookup of api.anthropic...  ->  [exit 0] NO_EGRESS
  [OK ] host escape: Docker Engine socket is absent    ->  [exit 0] ABSENT
  [OK ] host escape: host filesystem is not mounted    ->  [exit 0] CONTAINED

RESULT: PASS -- benign code ran; every escape attempt was contained.
```

`run.sh --keep` leaves the demo running; `run.sh --attach` reuses an already-up
demo control-plane.

## Use a real LLM

Set `OPENAI_API_KEY` and `run.sh` drives a real tool-calling agent
(`@ai-sdk/openai`, already installed) instead of the scripted probes. The tool —
and therefore the isolation — is identical.

## How it works

- [`ironclaw-sandbox.ts`](../_shared/ironclaw-sandbox.ts) — engages a per-session
  IronClaw sandbox (`ic-sbx-*`) against the demo control-plane and runs commands
  inside it as its own non-root uid. Pure Node standard library (global `fetch` +
  `child_process`).
- [`ironclaw-tool.ts`](ironclaw-tool.ts) — wraps that sandbox as a Vercel AI SDK
  `tool()` named `sandboxedShell`. **This is the ~15 lines you copy** to give any
  AI SDK agent a sandboxed shell.
- [`run.ts`](run.ts) — engages the sandbox and drives the tool.

The execution primitive is the same one IronClaw's red-team harness attacks: a
`docker exec` into the live per-session sandbox as its non-root uid. See the repo
root [`README`](../../../README.md) for the full isolation model.
