# Framework integrations

Give your agent framework a **sandboxed** place to run untrusted, model-generated
code. Each integration wraps the framework's own code-execution tool so commands
run inside a real IronClaw per-session sandbox — **no network, no host
filesystem, no Docker socket** — instead of on your host.

The typical LangChain / CrewAI setup hands the model a host shell or code
interpreter (`ShellTool`, `CodeInterpreterTool`, ...). One prompt injection or a
confidently-wrong tool call and that runs on your machine. These examples swap
that foot-gun for a boundary IronClaw [proves holds](../red-team-escape/).

| Framework | Directory | One-command demo |
|-----------|-----------|------------------|
| [LangChain](langchain/) | `examples/integrations/langchain/` | `examples/integrations/langchain/run.sh` |
| [CrewAI](crewai/) | `examples/integrations/crewai/` | `examples/integrations/crewai/run.sh` |

Both are **zero-credential**: the LLM side uses the offline mock provider, the
sandbox is real. Each demo engages a live sandbox, drives the framework's tool
over one benign task plus a battery of escape attempts, and prints a PASS/FAIL
containment table (exits non-zero if any containment expectation fails, so they
double as CI checks). Set `OPENAI_API_KEY` to drive a real LLM instead — the tool
and the isolation are identical.

## The pattern

The IronClaw-specific piece is one shared, standard-library client —
[`_shared/ironclaw_sandbox.py`](_shared/ironclaw_sandbox.py) — that engages a
per-session sandbox and runs commands inside it. Each framework adds a thin
adapter (~15-20 lines) turning that into a native tool:

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool

with IronClawSandbox() as sandbox:
    tool = make_sandbox_tool(sandbox)   # a real LangChain / CrewAI tool
    # ... hand `tool` to your agent in place of the host shell tool
```

Adding another framework is the same shape: reuse `ironclaw_sandbox.py`, write a
small `ironclaw_tool.py` adapter for that framework's tool interface.
