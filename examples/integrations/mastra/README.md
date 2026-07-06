# Mastra agents, sandboxed by IronClaw

Your [Mastra](https://mastra.ai) agent runs untrusted, model-generated code.
Point that execution at an **IronClaw sandbox** instead of your host and the same
agent gets real code execution with **no network, no host filesystem, and no
Docker socket** — the isolation boundary IronClaw
[proves holds](../../red-team-escape/), not just promises.

```ts
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

const sandbox = await new IronClawSandbox().engage();  // launch a per-session sandbox
const tool = makeSandboxTool(sandbox);                 // a real Mastra tool
console.log(await tool.execute({ command: "id" }));    // runs INSIDE the box, not your host
// const agent = new Agent({ tools: { sandboxedShell: tool }, ... });  // ... any Mastra agent
```

## Try it in one command

Zero credentials — the LLM side uses the offline **mock provider**, the sandbox
is real (needs Node.js 22+; `run.sh` installs the deps):

```sh
examples/integrations/mastra/run.sh
```

It engages a live IronClaw per-session sandbox, drives the `sandboxedShell`
tool's `execute` exactly as Mastra's agent loop would, runs one benign task plus
a battery of escape attempts, and prints a PASS/FAIL containment table:

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

Set `OPENAI_API_KEY` and `run.sh` drives a real Mastra `Agent` (the provider is
already bundled) instead of the scripted probes. The tool — and therefore the
isolation — is identical; the key never enters the sandbox.

## How it works

- [`ironclaw-sandbox.ts`](../_shared/ironclaw-sandbox.ts) — engages a per-session
  IronClaw sandbox (`ic-sbx-*`) against the demo control-plane and runs commands
  inside it as its own non-root uid. Pure Node standard library (`fetch` +
  `child_process`).
- [`ironclaw-tool.ts`](ironclaw-tool.ts) — wraps that sandbox as a Mastra tool
  (`createTool`) named `sandboxed_shell`. **This is the ~20 lines you copy** to
  swap a host shell tool for a sandboxed one.
- [`run.ts`](run.ts) — engages the sandbox and drives the tool.

This is the TypeScript twin of the Python integration examples — same
`ic-sbx-*` container, same escape battery
([`_shared/containment-demo.ts`](../_shared/containment-demo.ts) mirrors the
Python probes). The execution primitive is the same one IronClaw's red-team
harness attacks: a `docker exec` into the live per-session sandbox as its
non-root uid. See the repo root [`README`](../../../README.md) for the full
isolation model.
