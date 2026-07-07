"""Google ADK tool backed by an IronClaw sandbox.

Drop-in replacement for the host-executing code/shell tool you would normally
hand a **Google ADK** (Agent Development Kit) agent (a `FunctionTool` wrapping a
shell callable, the built-in code executor, ...): the agent calls it exactly the
same way, but every command runs inside an isolated IronClaw per-session sandbox
instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real ADK FunctionTool
    agent = Agent(name="coder", model=..., tools=[tool])
"""

from __future__ import annotations

from google.adk.tools import FunctionTool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> FunctionTool:
    """Wrap a live IronClaw sandbox as a Google ADK FunctionTool.

    The sandbox handle is captured in a closure so the wrapped callable is a
    plain function ADK can introspect for its automatic function-declaration
    schema, robust across google-adk versions.
    """

    def sandboxed_shell(command: str) -> str:
        """Run a shell command inside the IronClaw sandbox and return its output.

        Args:
            command: The shell command to run inside the sandbox.

        Returns:
            The command's combined stdout/stderr prefixed with its exit code.
        """
        return str(sandbox.run(command))

    return FunctionTool(func=sandboxed_shell)
