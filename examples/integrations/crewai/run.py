#!/usr/bin/env python3
"""Run a CrewAI agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the CrewAI
`sandboxed_shell` tool exactly as a crew agent would -- one benign task plus a
battery of escape attempts -- then prints a PASS/FAIL containment table.

Set OPENAI_API_KEY to instead run a real LLM-driven crew; the tool (and
therefore the isolation) is identical either way.
"""

from __future__ import annotations

import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def drive_with_real_crew(tool) -> int:
    """Optional: run a real LLM-driven crew that uses the sandbox tool."""
    from crewai import Agent, Crew, Task

    coder = Agent(
        role="Sandboxed coder",
        goal="Run commands the user asks for, safely.",
        backstory="You run everything inside an isolated IronClaw sandbox.",
        tools=[tool],
        verbose=True,
    )
    task = Task(
        description="Run `id` and report which user the sandbox runs as.",
        expected_output="The uid/gid the sandbox process runs as.",
        agent=coder,
    )
    result = Crew(agents=[coder], tasks=[task], verbose=True).kickoff()
    print(result)
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: running a real CrewAI crew")
            return drive_with_real_crew(tool)

        # Deterministic, zero-cred path: call the tool exactly as CrewAI does --
        # tool.run(command=...) -- for each probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: tool.run(command=command),
            framework="CrewAI",
        )


if __name__ == "__main__":
    raise SystemExit(main())
