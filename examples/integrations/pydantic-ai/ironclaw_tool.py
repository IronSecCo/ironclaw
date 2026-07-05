"""Pydantic AI tool backed by an IronClaw sandbox.

Drop-in replacement for a host-executing shell/code tool on a Pydantic AI agent
(a `@agent.tool_plain` that shells out, a code interpreter, ...): the agent calls
it exactly the same way, but every command runs inside an isolated IronClaw
per-session sandbox instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real Pydantic AI Tool
    agent = Agent("openai:gpt-4o", tools=[tool])   # ... plug into any agent
"""

from __future__ import annotations

from pydantic_ai import Tool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> Tool:
    """Wrap a live IronClaw sandbox as a Pydantic AI Tool.

    The sandbox handle is captured in a closure, so the tool exposes exactly one
    argument (`command`) to the agent -- the same surface a host shell tool would.
    Pydantic AI derives the JSON schema Pydantic AI hands the model from the
    function signature and docstring.
    """

    def sandboxed_shell(command: str) -> str:
        """Run a shell command inside the isolated IronClaw sandbox.

        Args:
            command: The shell command to run inside the sandbox.
        """
        return str(sandbox.run(command))

    return Tool(sandboxed_shell, name="sandboxed_shell", description=_DESCRIPTION)
