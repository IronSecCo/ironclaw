# OpenAI Agents SDK → IronClaw sandbox

Your OpenAI Agents SDK tools run in *your* process, with *your* key in memory and open
egress. This example gives the agent one tool — `sandbox_bash` — that executes inside a
real, sealed IronClaw sandbox instead: **no network card, no host filesystem, no Docker
socket.** Same agent you designed; a perimeter it never had.

## Run it (zero credentials)

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
examples/integrations/openai-agents/run.sh
```

That brings up the offline demo control-plane (mock provider — no API key), engages a
real per-session sandbox, runs a benign command through the SDK's tool, then plays out a
prompt-injected escape and prints each attempt **BLOCKED**. Exits non-zero if the box
ever leaks, so it doubles as a CI smoke.

## The snippet

The tool is genuine OpenAI Agents SDK — only its body changes: instead of running on the
host, it runs inside the sandbox.

```python
from agents import Agent, Runner, function_tool
from ironclaw_sandbox import SandboxSession

session = SandboxSession.engage()            # launch a real, sealed sandbox

@function_tool
def sandbox_bash(command: str) -> str:
    """Run a shell command inside the sealed IronClaw sandbox."""
    rc, out = session.exec(command)          # executes in the box, not on your host
    return f"(exit {rc})\n{out}"

agent = Agent(name="Sandboxed Operator", tools=[sandbox_bash], model="gpt-4o-mini")
Runner.run_sync(agent, "Show me this environment and prove it is isolated.")
```

Run the **real** SDK loop (the model plans the tool calls):

```sh
pip install openai-agents
export OPENAI_API_KEY=sk-...                  # host-side; the sandbox never sees it
examples/integrations/openai-agents/run.sh
```

## See also

- [`claude-agent-sdk/`](../claude-agent-sdk/) — the same pattern for the Claude Agent SDK.
- [`examples/live-containment`](../../live-containment/) — the escape probes, narrated.
- [`examples/red-team-escape`](../../red-team-escape/) — the full six-assertion battery.
- [Integrations docs](https://ironsecco.github.io/ironclaw/integrations/).
