#!/usr/bin/env python3
"""Run a Semantic Kernel agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the Semantic
Kernel `sandboxed_shell` kernel function exactly as the kernel would when a model
picks it -- one benign task plus a battery of escape attempts -- then prints a
PASS/FAIL containment table.

Set OPENAI_API_KEY to instead let a real LLM-driven kernel decide what to run; the
plugin (and therefore the isolation) is identical either way.
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


def drive_with_real_agent(plugin) -> int:
    """Optional: let a real LLM-driven kernel decide what to run (needs a key)."""
    from semantic_kernel import Kernel
    from semantic_kernel.connectors.ai.function_choice_behavior import (
        FunctionChoiceBehavior,
    )
    from semantic_kernel.connectors.ai.open_ai import (
        OpenAIChatCompletion,
        OpenAIChatPromptExecutionSettings,
    )

    async def _run() -> None:
        kernel = Kernel()
        kernel.add_service(OpenAIChatCompletion(ai_model_id="gpt-4o-mini"))
        kernel.add_plugin(plugin, plugin_name="ironclaw")
        settings = OpenAIChatPromptExecutionSettings(
            function_choice_behavior=FunctionChoiceBehavior.Auto()
        )
        answer = await kernel.invoke_prompt(
            "Run `id` with the sandboxed_shell function and tell me which user "
            "the sandbox runs as.",
            arguments=None,
            settings=settings,
        )
        print(answer)

    asyncio.run(_run())
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        plugin = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real Semantic Kernel")
            return drive_with_real_agent(plugin)

        # Deterministic, zero-cred path: call the kernel function exactly as SK's
        # function-calling loop does -- plugin.sandboxed_shell(command=...) -- per
        # probe.
        print(f"==> driving the 'sandboxed_shell' function over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: plugin.sandboxed_shell(command=command),
            framework="Semantic Kernel",
        )


if __name__ == "__main__":
    raise SystemExit(main())
