# Framework & agent-SDK integrations

Give the agent you built — in an agent framework or an agent SDK — a **sandboxed**
place to run untrusted, model-generated code. Each example wraps the framework's own
code/tool execution so commands run inside a real IronClaw per-session sandbox — **no
network, no host filesystem, no Docker socket** — instead of on your host. The
framework still plans the tool calls; only the execution moves into the box.

The typical setup hands the model a host shell or code interpreter (`ShellTool`,
`CodeInterpreterTool`, a `run_shell` function, ...). One prompt injection or a
confidently-wrong tool call and that runs on your machine. These examples swap that
foot-gun for a boundary IronClaw [proves holds](../red-team-escape/).

| Example | Framework / SDK | One-command demo |
|---------|-----------------|------------------|
| [`langchain/`](langchain/) | [LangChain](https://python.langchain.com) | `examples/integrations/langchain/run.sh` |
| [`langgraph/`](langgraph/) | [LangGraph](https://langchain-ai.github.io/langgraph/) | `examples/integrations/langgraph/run.sh` |
| [`crewai/`](crewai/) | [CrewAI](https://docs.crewai.com) | `examples/integrations/crewai/run.sh` |
| [`agno/`](agno/) | [Agno (ex-Phidata)](https://docs.agno.com) | `examples/integrations/agno/run.sh` |
| [`llamaindex/`](llamaindex/) | [LlamaIndex](https://docs.llamaindex.ai) | `examples/integrations/llamaindex/run.sh` |
| [`pydantic-ai/`](pydantic-ai/) | [Pydantic AI](https://ai.pydantic.dev) | `examples/integrations/pydantic-ai/run.sh` |
| [`autogen/`](autogen/) | [AutoGen (Microsoft)](https://github.com/microsoft/autogen) | `examples/integrations/autogen/run.sh` |
| [`semantic-kernel/`](semantic-kernel/) | [Semantic Kernel (Microsoft)](https://github.com/microsoft/semantic-kernel) | `examples/integrations/semantic-kernel/run.sh` |
| [`smolagents/`](smolagents/) | [smolagents (HuggingFace)](https://github.com/huggingface/smolagents) | `examples/integrations/smolagents/run.sh` |
| [`google-adk/`](google-adk/) | [Google ADK](https://github.com/google/adk-python) | `examples/integrations/google-adk/run.sh` |
| [`dspy/`](dspy/) | [DSPy (Stanford)](https://github.com/stanfordnlp/dspy) | `examples/integrations/dspy/run.sh` |
| [`openai-agents/`](openai-agents/) | [OpenAI Agents SDK](https://github.com/openai/openai-agents-python) | `examples/integrations/openai-agents/run.sh` |
| [`claude-agent-sdk/`](claude-agent-sdk/) | [Claude Agent SDK](https://github.com/anthropics/claude-agent-sdk-python) | `examples/integrations/claude-agent-sdk/run.sh` |
| [`vercel-ai-sdk/`](vercel-ai-sdk/) | [Vercel AI SDK](https://ai-sdk.dev/) *(TS)* | `examples/integrations/vercel-ai-sdk/run.sh` |
| [`langchainjs/`](langchainjs/) | [LangChain.js](https://js.langchain.com/) *(TS)* | `examples/integrations/langchainjs/run.sh` |
| [`mastra/`](mastra/) | [Mastra](https://mastra.ai/) *(TS)* | `examples/integrations/mastra/run.sh` |

Every one is **zero-credential**: the LLM side uses the offline `mock` provider (or a
scripted transcript), the sandbox is real. Each demo engages a live sandbox, drives the
framework's tool over one benign task plus a battery of escape attempts, and prints a
PASS/FAIL containment table — exiting non-zero if any containment expectation fails, so
they double as CI checks. Set `OPENAI_API_KEY` (or `pip install` the SDK + its key,
host-side) to drive a real LLM instead — the tool and the isolation are identical; the
key never enters the sandbox.

## Low-code / automation: n8n community node

[`n8n/`](n8n/) is the odd one out — a publishable **n8n community node**
(`n8n-nodes-ironclaw`), not a framework tool. It reaches the automation/ops crowd
who run AI or code steps inside [n8n](https://n8n.io/) workflows and need to
sandbox untrusted execution. It talks to `ironctl mcp serve` (the `sandbox_exec`
MCP tool) rather than a per-session `ic-sbx-*` container, so it builds/lints with
the n8n scaffold and ships an import-ready example workflow. Its containment smoke
requires gVisor — see [`n8n/README.md`](n8n/README.md).

## The pattern

The IronClaw-specific piece is one small, standard-library client that engages a
per-session sandbox and runs commands inside it. The examples come in two shapes:

- **Framework tools** (LangChain, LangGraph, CrewAI, Agno, LlamaIndex, Pydantic AI,
  AutoGen, Semantic Kernel, smolagents, Google ADK, DSPy) wrap the shared
  [`_shared/ironclaw_sandbox.py`](_shared/ironclaw_sandbox.py) client as a native tool
  via a thin `ironclaw_tool.py` adapter (~15-20 lines):

  ```python
  from ironclaw_sandbox import IronClawSandbox
  from ironclaw_tool import make_sandbox_tool

  with IronClawSandbox() as sandbox:
      tool = make_sandbox_tool(sandbox)   # a real LangChain / CrewAI tool
      # ... hand `tool` to your agent in place of the host shell tool
  ```

- **Agent SDKs** (OpenAI Agents, Claude Agent) back their code/bash tool with a sealed
  session from [`ironclaw_sandbox.py`](ironclaw_sandbox.py) — same idea, SDK-shaped:

  ```python
  from ironclaw_sandbox import SandboxSession

  session = SandboxSession.engage()       # launch a real per-session sandbox
  rc, out = session.exec("uname -a")      # the SDK's tool runs THIS, inside the box
  ```

Both clients engage the same per-session container (`ic-sbx-*`) and exec into it as its
own unprivileged uid (65532) — the exact privilege a fully-jailbroken agent would have.
Each demo ends by running three real escapes (network exfil, host-filesystem read,
Docker-socket takeover) and reporting each **BLOCKED** — the same probes as
[`examples/live-containment`](../live-containment/).

Adding another framework is the same shape: reuse the client, write a small adapter for
that framework's tool interface.

### JS / TypeScript

The [`vercel-ai-sdk/`](vercel-ai-sdk/), [`langchainjs/`](langchainjs/), and
[`mastra/`](mastra/) examples are
the TypeScript twins of the pattern above, for the npm agent ecosystem. They wrap the
shared, dependency-free [`_shared/ironclaw-sandbox.ts`](_shared/ironclaw-sandbox.ts)
client (global `fetch` + `child_process`) as a native tool:

```ts
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

const sandbox = await new IronClawSandbox().engage();  // launch a real per-session sandbox
const tool = makeSandboxTool(sandbox);                 // a real AI SDK / LangChain.js tool
console.log(await tool.execute({ command: "id" }, ctx)); // runs INSIDE the box, not your host
```

They engage the same `ic-sbx-*` container and run the identical escape battery
(`_shared/containment-demo.ts` mirrors the Python probes), exiting non-zero on any
containment miss — so the smoke matrix asserts them with the same rigor.

## Prerequisites

- Docker (the sandbox is a real container). The Python examples need `python3` (stdlib
  only — no `pip install` for the default, credential-free path); the JS/TS examples need
  Node.js 18+ (their only runtime dep, the framework, is installed by `run.sh`).
- For the real runners: install the framework/SDK if needed (`pip install langchain` /
  `crewai` / `openai-agents` / `claude-agent-sdk`; the JS examples already bundle their
  provider) and set its API key **host-side** (the sandbox never sees it).

## See also

- [`examples/live-containment`](../live-containment/) — watch the escape probes, narrated.
- [`examples/red-team-escape`](../red-team-escape/) — the full six-assertion battery + report.
- [Integrations docs](https://ironsecco.github.io/ironclaw/integrations/) — one "Sandbox your <framework> agent" guide per framework.
