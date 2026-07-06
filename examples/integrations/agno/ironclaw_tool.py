"""Agno toolkit backed by an IronClaw sandbox.

Agno (ex-Phidata) agents get tools from an `agno.tools.Toolkit`: you subclass it,
write methods with type hints + docstrings (Agno turns them into the LLM tool
schema), and hand the toolkit to `Agent(tools=[...])`. This is a drop-in
replacement for a host-executing shell tool — the agent calls `sandboxed_shell`
exactly the same way, but every command runs inside an isolated IronClaw
per-session sandbox instead of on your machine.

    from ironclaw_tool import IronClawTools
    from ironclaw_sandbox import IronClawSandbox
    from agno.agent import Agent

    sandbox = IronClawSandbox().__enter__()
    agent = Agent(tools=[IronClawTools(sandbox)])   # ... plug into any Agno agent
"""

from __future__ import annotations

from agno.tools import Toolkit

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. The sandbox has no network, no access to the "
    "host filesystem, and no Docker socket, so it is safe to run untrusted code."
)


class IronClawTools(Toolkit):
    """An Agno toolkit whose one tool runs commands inside an IronClaw sandbox."""

    def __init__(self, sandbox: IronClawSandbox, **kwargs) -> None:
        self._sandbox = sandbox
        super().__init__(
            name="ironclaw_sandbox",
            tools=[self.sandboxed_shell],
            **kwargs,
        )

    def sandboxed_shell(self, command: str) -> str:
        """Execute a shell command inside an isolated IronClaw sandbox.

        Use this for any code the user asks you to run. The sandbox has no
        network, no host filesystem, and no Docker socket, so untrusted commands
        are safe to run.

        Args:
            command (str): The shell command to run inside the sandbox.

        Returns:
            str: The command's combined stdout/stderr, prefixed with its exit code.
        """
        return str(self._sandbox.run(command))
