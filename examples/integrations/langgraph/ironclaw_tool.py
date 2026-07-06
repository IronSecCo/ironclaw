"""LangGraph tool backed by an IronClaw sandbox.

LangGraph agents execute tools through a `ToolNode` (or the prebuilt
`create_react_agent`); a LangGraph tool is just a LangChain `BaseTool`. This is a
drop-in replacement for a host-executing shell / code tool: the graph calls it
exactly the same way, but every command runs inside an isolated IronClaw
per-session sandbox instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox
    from langgraph.prebuilt import ToolNode, create_react_agent

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real LangChain BaseTool
    tool_node = ToolNode([tool])               # ... wire into any LangGraph graph
    agent = create_react_agent(llm, [tool])    # ... or the prebuilt ReAct graph
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
    """Wrap a live IronClaw sandbox as a LangChain StructuredTool for LangGraph."""

    def _run(command: str) -> str:
        return str(sandbox.run(command))

    return StructuredTool.from_function(
        func=_run,
        name="sandboxed_shell",
        description=_DESCRIPTION,
        args_schema=SandboxCommand,
    )
