#!/usr/bin/env python3
"""Run an Agno agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the Agno
toolkit's `sandboxed_shell` tool exactly as an Agno agent would call it -- one
benign task plus a battery of escape attempts -- then prints a PASS/FAIL
containment table.

Set OPENAI_API_KEY to instead let a real LLM-driven Agno agent decide what to
run; the tool (and therefore the isolation) is identical either way.
"""

from __future__ import annotations

import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import IronClawTools  # noqa: E402


def drive_with_real_agent(toolkit: IronClawTools) -> int:
    """Optional: let a real LLM-driven Agno agent decide what to run (needs a key)."""
    from agno.agent import Agent
    from agno.models.openai import OpenAIChat

    agent = Agent(model=OpenAIChat(id="gpt-4o-mini"), tools=[toolkit])
    agent.print_response("Run `id` and tell me which user the sandbox runs as.")
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        toolkit = IronClawTools(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real Agno agent")
            return drive_with_real_agent(toolkit)

        # Deterministic, zero-cred path: call the toolkit's registered tool
        # exactly as Agno's function-calling loop does -- sandboxed_shell(command)
        # -- for each probe.
        print(f"==> driving the 'sandboxed_shell' tool over {len(PROBES)} agent commands")
        return run_containment_demo(toolkit.sandboxed_shell, framework="Agno")


if __name__ == "__main__":
    raise SystemExit(main())
