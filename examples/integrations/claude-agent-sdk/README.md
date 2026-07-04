# Claude Agent SDK → IronClaw sandbox

The Claude Agent SDK ships a powerful bash/code capability that, by default, runs on your
host with your key in memory. This example backs that capability with a real, sealed
IronClaw sandbox session: **no network card, no host filesystem, no Docker socket.** The
agent you built with Claude, inside a box a jailbroken agent can't escape.

## Run it (zero credentials)

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
examples/integrations/claude-agent-sdk/run.sh
```

That brings up the offline demo control-plane (mock provider — no API key), engages a
real per-session sandbox, runs a benign command through the SDK's bash tool, then plays
out a prompt-injected escape and prints each attempt **BLOCKED**. Exits non-zero if the
box ever leaks, so it doubles as a CI smoke.

## The snippet

The tool wiring is genuine Claude Agent SDK — only the handler body changes: it runs the
command inside the sandbox instead of on the host.

```python
from claude_agent_sdk import tool, create_sdk_mcp_server, ClaudeAgentOptions
from ironclaw_sandbox import SandboxSession

session = SandboxSession.engage()            # launch a real, sealed sandbox

@tool("sandbox_bash", "Run a shell command inside a sealed IronClaw sandbox.",
      {"command": str})
async def sandbox_bash(args):
    rc, out = session.exec(args["command"])  # executes in the box, not on your host
    return {"content": [{"type": "text", "text": f"(exit {rc})\n{out}"}]}

server = create_sdk_mcp_server(name="ironclaw", version="1.0.0", tools=[sandbox_bash])
options = ClaudeAgentOptions(mcp_servers={"ironclaw": server},
                             allowed_tools=["mcp__ironclaw__sandbox_bash"])
```

Run the **real** SDK loop (Claude plans the tool calls):

```sh
pip install claude-agent-sdk
export ANTHROPIC_API_KEY=sk-ant-...           # host-side; the sandbox never sees it
examples/integrations/claude-agent-sdk/run.sh
```

## See also

- [`openai-agents/`](../openai-agents/) — the same pattern for the OpenAI Agents SDK.
- [`examples/live-containment`](../../live-containment/) — the escape probes, narrated.
- [`examples/red-team-escape`](../../red-team-escape/) — the full six-assertion battery.
- [Integrations docs](https://ironsecco.github.io/ironclaw/integrations/).
