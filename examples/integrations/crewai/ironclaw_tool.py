"""CrewAI tool backed by an IronClaw sandbox.

Drop-in replacement for CrewAI's host-executing code tools (e.g.
`crewai_tools.CodeInterpreterTool` / any shell tool): a crew agent calls it the
same way, but every command runs inside an isolated IronClaw per-session sandbox
instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real CrewAI BaseTool
    agent = Agent(role="coder", tools=[tool], ...)
"""

from __future__ import annotations

from crewai.tools import BaseTool
from pydantic import BaseModel, Field

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this to run any code. The sandbox has no "
    "network, no host filesystem access, and no Docker socket, so untrusted "
    "commands are safe to run."
)


class SandboxCommand(BaseModel):
    command: str = Field(description="The shell command to run inside the sandbox.")


def make_sandbox_tool(sandbox: IronClawSandbox) -> BaseTool:
    """Wrap a live IronClaw sandbox as a CrewAI BaseTool.

    The sandbox handle is captured in a closure so it does not have to be a
    pydantic field on the tool (which keeps this robust across CrewAI versions).
    """

    class IronClawSandboxTool(BaseTool):
        name: str = "sandboxed_shell"
        description: str = _DESCRIPTION
        args_schema: type[BaseModel] = SandboxCommand

        def _run(self, command: str) -> str:
            return str(sandbox.run(command))

    return IronClawSandboxTool()
