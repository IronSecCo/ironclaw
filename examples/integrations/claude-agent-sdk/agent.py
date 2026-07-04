#!/usr/bin/env python3
"""Claude Agent SDK agent whose bash tool is backed by an IronClaw sandbox session.

The value: the Claude Agent SDK ships a powerful bash/code capability. Point that
capability at a sealed IronClaw per-session sandbox and the model can run whatever it
likes with none of the blast radius — no network card, no host filesystem, no Docker
socket. The agent you designed with Claude, inside a box a jailbroken agent can't escape.

Two ways to run, picked automatically:

* Zero-credential (default). No ``claude-agent-sdk`` install and no ``ANTHROPIC_API_KEY``
  needed: we drive the same sandbox-backed tool through a short scripted transcript (a
  "mock LLM") so you can watch the sealed loop — a benign command that works, then an
  escape that is blocked — with nothing but Python + Docker.
* Real SDK. If ``claude_agent_sdk`` is importable and ``ANTHROPIC_API_KEY`` is set, the
  real agent loop runs and Claude decides when to call the sandbox-backed bash tool.

Either way the tool wiring below is genuine Claude Agent SDK usage.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from ironclaw_sandbox import (  # noqa: E402
    BOLD, CYAN, DIM, GREEN, RESET, YELLOW,
    SandboxSession, print_containment,
)

_SESSION: SandboxSession | None = None


def _session() -> SandboxSession:
    global _SESSION
    if _SESSION is None:
        print(f"{DIM}==> engaging a real IronClaw sandbox (per-session container)...{RESET}")
        _SESSION = SandboxSession.engage()
        print(f"{DIM}    sandbox up: {_SESSION.container}{RESET}")
    return _SESSION


def _run_in_sandbox(command: str) -> str:
    rc, out = _session().exec(command)
    return f"(exit {rc})\n{out}" if out else f"(exit {rc}, no output)"


# --- the tool: genuine Claude Agent SDK, backed by the sandbox --------------
# `claude_agent_sdk` is the Claude Agent SDK (pip install claude-agent-sdk). Its `tool`
# decorator wraps a handler returning {"content": [...]}. When the SDK is not installed
# we register a plain callable so the SAME handler runs under the zero-credential path.
try:
    from claude_agent_sdk import tool  # type: ignore
    _HAVE_SDK = True

    @tool("sandbox_bash", "Run a shell command inside a sealed IronClaw sandbox "
          "(no network, no host access) and return its output.",
          {"command": str})
    async def sandbox_bash(args):  # type: ignore
        return {"content": [{"type": "text", "text": _run_in_sandbox(args["command"])}]}

except ImportError:  # zero-credential path
    _HAVE_SDK = False

    def sandbox_bash(command: str) -> str:  # type: ignore
        return _run_in_sandbox(command)


def _speak_agent(text: str) -> None:
    print(f"{BOLD}{CYAN}Claude>{RESET} {text}")


def _speak_tool(command: str, result: str) -> None:
    print(f"{DIM}  -> sandbox_bash({command!r}){RESET}")
    for line in result.splitlines():
        print(f"{DIM}     {line}{RESET}")


# --- zero-credential scripted runner (mock LLM) -----------------------------
def run_scripted() -> int:
    print(f"\n{BOLD}Claude Agent SDK -> IronClaw sandbox{RESET} "
          f"{DIM}(zero-credential scripted transcript){RESET}\n")

    _speak_agent("I'll inspect this environment by running bash in the sandbox.")
    result = _run_in_sandbox("uname -a; id; echo sealed-workspace: $(pwd)")
    _speak_tool("uname -a; id; echo sealed-workspace: $(pwd)", result)
    _speak_agent("That executed inside the sealed sandbox - your host was never touched.")

    print(f"\n{YELLOW}Now a prompt-injected instruction makes the agent try to break out.{RESET}")
    probes = _session().containment_report()
    ok = print_containment(probes)

    _session().close()
    print()
    if ok:
        print(f"{GREEN}{BOLD}PASS{RESET} - Claude's bash tool ran real code in a sandbox that "
              f"a jailbroken agent could not escape.")
        return 0
    print("FAIL - an escape was not contained (see above).")
    return 1


# --- real Claude Agent SDK runner -------------------------------------------
def run_sdk() -> int:
    """Let the real Claude Agent SDK loop drive the sandbox-backed bash tool."""
    import asyncio

    from claude_agent_sdk import (  # type: ignore
        ClaudeAgentOptions, ClaudeSDKClient, create_sdk_mcp_server,
    )

    server = create_sdk_mcp_server(name="ironclaw", version="1.0.0", tools=[sandbox_bash])
    options = ClaudeAgentOptions(
        mcp_servers={"ironclaw": server},
        allowed_tools=["mcp__ironclaw__sandbox_bash"],
        system_prompt=(
            "Use the sandbox_bash tool for ALL shell/code execution. First report the "
            "box identity (uname, id, pwd). Then attempt to resolve api.anthropic.com "
            "and read /host/etc/shadow, and report plainly that both are blocked."
        ),
    )

    async def _drive() -> None:
        async with ClaudeSDKClient(options=options) as client:
            await client.query("Show me this environment and prove it is isolated.")
            async for message in client.receive_response():
                text = getattr(message, "result", None) or getattr(message, "content", None)
                if text:
                    print(f"{BOLD}{CYAN}Claude>{RESET} {text}")

    asyncio.run(_drive())

    probes = _session().containment_report()
    ok = print_containment(probes)
    _session().close()
    return 0 if ok else 1


def main() -> int:
    use_sdk = _HAVE_SDK and os.environ.get("ANTHROPIC_API_KEY")
    if use_sdk:
        print(f"{DIM}==> Claude Agent SDK detected + ANTHROPIC_API_KEY set: real loop.{RESET}")
        return run_sdk()
    if not _HAVE_SDK:
        print(f"{DIM}==> claude-agent-sdk not installed: zero-credential scripted transcript.{RESET}")
        print(f"{DIM}    (pip install claude-agent-sdk and set ANTHROPIC_API_KEY for the real loop.){RESET}")
    else:
        print(f"{DIM}==> ANTHROPIC_API_KEY unset: zero-credential scripted transcript.{RESET}")
    return run_scripted()


if __name__ == "__main__":
    raise SystemExit(main())
