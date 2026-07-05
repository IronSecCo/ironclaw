"""LlamaIndex tool backed by an IronClaw sandbox.

Drop-in replacement for LlamaIndex's host-executing code/shell tools (e.g. a
`FunctionTool` wrapping `subprocess.run`, or the code-interpreter tools): a
LlamaIndex agent calls it exactly the same way, but every command runs inside an
isolated IronClaw per-session sandbox instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real LlamaIndex FunctionTool
    agent = FunctionAgent(tools=[tool], llm=llm)   # ... plug into any agent
"""

from __future__ import annotations

from llama_index.core.tools import FunctionTool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> FunctionTool:
    """Wrap a live IronClaw sandbox as a LlamaIndex FunctionTool.

    The sandbox handle is captured in a closure, so the tool exposes exactly one
    argument (`command`) to the agent -- the same surface a host shell tool would.
    """

    def sandboxed_shell(command: str) -> str:
        """Run a shell command inside the isolated IronClaw sandbox."""
        return str(sandbox.run(command))

    return FunctionTool.from_defaults(
        fn=sandboxed_shell,
        name="sandboxed_shell",
        description=_DESCRIPTION,
    )
