"""smolagents tool backed by an IronClaw sandbox.

Drop-in replacement for the host-executing code/shell tool you would normally
hand a **smolagents** (HuggingFace) agent (a `Tool` subclass that shells out, the
built-in Python executor, ...): the agent calls it exactly the same way, but every
command runs inside an isolated IronClaw per-session sandbox instead of on your
machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real smolagents Tool
    agent = ToolCallingAgent(tools=[tool], model=...)
"""

from __future__ import annotations

from smolagents import Tool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> Tool:
    """Wrap a live IronClaw sandbox as a smolagents Tool.

    The sandbox handle is captured in a closure so it does not have to be a
    constructor field on the Tool, which keeps this robust across smolagents
    versions (the `Tool` base validates declared attributes on subclasses).
    """

    class IronClawSandboxTool(Tool):
        name = "sandboxed_shell"
        description = _DESCRIPTION
        inputs = {
            "command": {
                "type": "string",
                "description": "The shell command to run inside the sandbox.",
            }
        }
        output_type = "string"

        def forward(self, command: str) -> str:
            """Run a shell command inside the IronClaw sandbox and return its output."""
            return str(sandbox.run(command))

    return IronClawSandboxTool()
