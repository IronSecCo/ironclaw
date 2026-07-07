#!/usr/bin/env python3
"""Run a DSPy program whose tool execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the DSPy
`sandboxed_shell` tool exactly as a `dspy.ReAct` module would when the model
picks it -- one benign task plus a battery of escape attempts -- then prints a
PASS/FAIL containment table.

Set OPENAI_API_KEY to instead let a real LLM-driven DSPy program decide what to
run; the tool (and therefore the isolation) is identical either way.
"""

from __future__ import annotations

import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def drive_with_real_agent(tool) -> int:
    """Optional: let a real LLM-driven DSPy program decide what to run (needs a key)."""
    import dspy

    dspy.configure(lm=dspy.LM("openai/gpt-4o-mini"))
    program = dspy.ReAct("task -> answer", tools=[tool])
    result = program(
        task="Run `id` with the sandboxed_shell tool and tell me which user the "
        "sandbox runs as."
    )
    print(result.answer)
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real dspy.ReAct program")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: call the tool exactly as DSPy's ReAct
        # loop does -- tool(command=...) -- per probe.
        print(f"==> driving the '{tool.name}' tool over the containment probes")
        return run_containment_demo(
            lambda command: tool(command=command),
            framework="DSPy",
        )


if __name__ == "__main__":
    raise SystemExit(main())
