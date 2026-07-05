"""AutoGen tool backed by an IronClaw sandbox.

Drop-in replacement for the host-executing code/shell tools you would normally
hand an AutoGen `AssistantAgent` (a `FunctionTool` wrapping a `run_shell`
callable, a code-executor, ...): the agent calls it exactly the same way, but
every command runs inside an isolated IronClaw per-session sandbox instead of on
your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real AutoGen FunctionTool
    agent = AssistantAgent("coder", model_client=..., tools=[tool])
"""

from __future__ import annotations

from autogen_core.tools import FunctionTool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> FunctionTool:
    """Wrap a live IronClaw sandbox as an AutoGen FunctionTool.

    The sandbox handle is captured in a closure so the tool is a plain callable
    AutoGen can introspect and schedule, robust across autogen-core versions.
    """

    def sandboxed_shell(command: str) -> str:
        """Run a shell command inside the IronClaw sandbox and return its output."""
        return str(sandbox.run(command))

    return FunctionTool(
        sandboxed_shell,
        name="sandboxed_shell",
        description=_DESCRIPTION,
    )
