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
| [`crewai/`](crewai/) | [CrewAI](https://docs.crewai.com) | `examples/integrations/crewai/run.sh` |
| [`llamaindex/`](llamaindex/) | [LlamaIndex](https://docs.llamaindex.ai) | `examples/integrations/llamaindex/run.sh` |
| [`pydantic-ai/`](pydantic-ai/) | [Pydantic AI](https://ai.pydantic.dev) | `examples/integrations/pydantic-ai/run.sh` |
| [`openai-agents/`](openai-agents/) | [OpenAI Agents SDK](https://github.com/openai/openai-agents-python) | `examples/integrations/openai-agents/run.sh` |
| [`claude-agent-sdk/`](claude-agent-sdk/) | [Claude Agent SDK](https://github.com/anthropics/claude-agent-sdk-python) | `examples/integrations/claude-agent-sdk/run.sh` |

Every one is **zero-credential**: the LLM side uses the offline `mock` provider (or a
scripted transcript), the sandbox is real. Each demo engages a live sandbox, drives the
framework's tool over one benign task plus a battery of escape attempts, and prints a
PASS/FAIL containment table — exiting non-zero if any containment expectation fails, so
they double as CI checks. Set `OPENAI_API_KEY` (or `pip install` the SDK + its key,
host-side) to drive a real LLM instead — the tool and the isolation are identical; the
key never enters the sandbox.

## The pattern

The IronClaw-specific piece is one small, standard-library client that engages a
per-session sandbox and runs commands inside it. The examples come in two shapes:

- **Framework tools** (LangChain, CrewAI, LlamaIndex, Pydantic AI) wrap the shared
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

## Prerequisites

- Docker (the sandbox is a real container) and `python3` (stdlib only — no `pip install`
  for the default, credential-free path).
- For the real runners: install the framework/SDK (`pip install langchain` /
  `crewai` / `openai-agents` / `claude-agent-sdk`) and set its API key **host-side**
  (the sandbox never sees it).

## See also

- [`examples/live-containment`](../live-containment/) — watch the escape probes, narrated.
- [`examples/red-team-escape`](../red-team-escape/) — the full six-assertion battery + report.
- [Integrations docs](https://ironsecco.github.io/ironclaw/integrations/) — one "Sandbox your <framework> agent" guide per framework.
