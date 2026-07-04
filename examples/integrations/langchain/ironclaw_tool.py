"""LangChain tool backed by an IronClaw sandbox.

Drop-in replacement for LangChain's host-executing shell / code tools (e.g.
`langchain_community.tools.ShellTool`): the agent calls it exactly the same way,
but every command runs inside an isolated IronClaw per-session sandbox instead
of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real LangChain BaseTool
    agent = create_react_agent(llm, [tool])    # ... plug into any agent
"""

from __future__ import annotations

from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


class SandboxCommand(BaseModel):
    command: str = Field(description="The shell command to run inside the sandbox.")


def make_sandbox_tool(sandbox: IronClawSandbox) -> StructuredTool:
    """Wrap a live IronClaw sandbox as a LangChain StructuredTool."""

    def _run(command: str) -> str:
        return str(sandbox.run(command))

    return StructuredTool.from_function(
        func=_run,
        name="sandboxed_shell",
        description=_DESCRIPTION,
        args_schema=SandboxCommand,
    )
