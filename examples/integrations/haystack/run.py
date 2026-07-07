#!/usr/bin/env python3
"""Run a Haystack agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the Haystack
`sandboxed_shell` tool exactly as an agent would -- one benign task plus a
battery of escape attempts -- then prints a PASS/FAIL containment table.

Set OPENAI_API_KEY to instead run a real LLM-driven Haystack `Agent`; the tool
(and therefore the isolation) is identical either way.
"""

from __future__ import annotations

import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def drive_with_real_agent(tool) -> int:
    """Optional: run a real LLM-driven Haystack Agent that uses the sandbox tool."""
    from haystack.components.agents import Agent
    from haystack.components.generators.chat import OpenAIChatGenerator
    from haystack.dataclasses import ChatMessage

    agent = Agent(
        chat_generator=OpenAIChatGenerator(model="gpt-4o-mini"),
        tools=[tool],
        system_prompt=(
            "You run everything inside an isolated IronClaw sandbox via the "
            "sandboxed_shell tool. Never assume a command's output; run it."
        ),
    )
    result = agent.run(
        messages=[ChatMessage.from_user("Run `id` and report which user the sandbox runs as.")]
    )
    for message in result["messages"]:
        print(message.text or message)
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: running a real Haystack Agent")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: call the tool exactly as Haystack does --
        # tool.invoke(command=...) -- for each probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: str(tool.invoke(command=command)),
            framework="Haystack",
        )


if __name__ == "__main__":
    raise SystemExit(main())
