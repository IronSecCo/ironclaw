#!/usr/bin/env python3
"""Run a Google ADK agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the ADK
`sandboxed_shell` FunctionTool exactly as an `Agent`'s tool loop would when a
model picks it -- one benign task plus a battery of escape attempts -- then prints
a PASS/FAIL containment table.

Set GOOGLE_API_KEY to instead let a real Gemini-driven `Agent` decide what to run;
the tool (and therefore the isolation) is identical either way.
"""

from __future__ import annotations

import asyncio
import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def _invoke_tool(tool, command: str) -> str:
    """Call the FunctionTool exactly as ADK's tool loop does: run_async(args=...)."""

    async def _call() -> str:
        result = await tool.run_async(args={"command": command}, tool_context=None)
        return str(result)

    return asyncio.run(_call())


def drive_with_real_agent(tool) -> int:
    """Optional: let a real Gemini-driven ADK Agent decide what to run (needs a key)."""
    from google.adk.agents import Agent
    from google.adk.runners import InMemoryRunner
    from google.genai import types

    async def _run() -> None:
        agent = Agent(
            name="sandboxed_coder",
            model="gemini-2.0-flash",
            instruction="Run commands with the sandboxed_shell tool to answer.",
            tools=[tool],
        )
        runner = InMemoryRunner(agent=agent, app_name="ironclaw")
        session = await runner.session_service.create_session(
            app_name="ironclaw", user_id="demo"
        )
        message = types.Content(
            role="user",
            parts=[types.Part(text="Run `id` and tell me which user the sandbox runs as.")],
        )
        async for event in runner.run_async(
            user_id="demo", session_id=session.id, new_message=message
        ):
            if event.is_final_response() and event.content:
                print("".join(p.text or "" for p in event.content.parts))

    asyncio.run(_run())
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("GOOGLE_API_KEY"):
            print("==> GOOGLE_API_KEY set: driving a real Gemini Agent")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: call the FunctionTool exactly as ADK's
        # tool loop does -- run_async(args={"command": ...}) -- per probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: _invoke_tool(tool, command),
            framework="Google ADK",
        )


if __name__ == "__main__":
    raise SystemExit(main())
