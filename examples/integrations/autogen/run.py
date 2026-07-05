#!/usr/bin/env python3
"""Run an AutoGen agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the AutoGen
`sandboxed_shell` FunctionTool exactly as an `AssistantAgent` would -- one benign
task plus a battery of escape attempts -- then prints a PASS/FAIL containment
table.

Set OPENAI_API_KEY to instead let a real LLM-driven AssistantAgent decide what to
run; the tool (and therefore the isolation) is identical either way.
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
    """Call the FunctionTool exactly as AutoGen's tool loop does: run_json + stringify."""
    from autogen_core import CancellationToken

    async def _call() -> str:
        result = await tool.run_json({"command": command}, CancellationToken())
        return tool.return_value_as_string(result)

    return asyncio.run(_call())


def drive_with_real_agent(tool) -> int:
    """Optional: let a real LLM-driven AssistantAgent decide what to run (needs a key)."""
    from autogen_agentchat.agents import AssistantAgent
    from autogen_ext.models.openai import OpenAIChatCompletionClient

    async def _run() -> None:
        model_client = OpenAIChatCompletionClient(model="gpt-4o-mini")
        agent = AssistantAgent(
            "sandboxed_coder",
            model_client=model_client,
            tools=[tool],
            system_message="Run commands with the sandboxed_shell tool to answer.",
        )
        result = await agent.run(
            task="Run `id` and tell me which user the sandbox runs as."
        )
        print(result.messages[-1].content)
        await model_client.close()

    asyncio.run(_run())
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real AssistantAgent")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: call the FunctionTool exactly as an
        # AssistantAgent's tool loop does -- run_json({"command": ...}) -- per probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: _invoke_tool(tool, command),
            framework="AutoGen",
        )


if __name__ == "__main__":
    raise SystemExit(main())
