"""Letta (MemGPT) tool backed by an IronClaw sandbox.

Drop-in replacement for the host-executing shell / code tool you would otherwise
give a **Letta** (formerly MemGPT) agent: the agent calls it exactly the same
way, but every command runs inside an isolated IronClaw per-session sandbox
instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a Letta client-side tool

Letta's execution model matters here. By default Letta serializes a tool's
Python *source* and runs it inside its own tool sandbox on the Letta server, so a
closure that captures a live sandbox handle cannot survive registration. Letta's
**client-side tool execution** is the honest fit: you pass the tool *schema* to
the agent and, when the model calls it, your own process executes the command --
here, against the live IronClaw sandbox -- and returns the result. So this tool
is a plain object with two faces:

  * ``tool.schema`` -- the ``client_tools`` entry you hand to
    ``client.agents.messages.create(..., client_tools=[tool.schema])``.
  * ``tool(command=...)`` -- the executor your client dispatches the model's
    tool call to. It is also how the credential-free containment demo drives it.

No ``letta`` import is needed to build the tool; the schema is a plain dict.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this to run any code the user asks you to "
    "run. The sandbox has no network, no access to the host filesystem, and no "
    "Docker socket, so it is safe to run untrusted commands."
)


@dataclass
class LettaSandboxTool:
    """A Letta client-side tool whose execution lands in an IronClaw sandbox.

    Callable as ``tool(command=...)`` (how your client dispatches the model's
    tool call, and how the containment demo drives it) and exposing ``.schema``
    (the ``client_tools`` entry you pass to the messages API).
    """

    name: str
    description: str
    _run: Callable[[str], str]

    @property
    def schema(self) -> dict:
        """The function schema Letta forwards to the model as a callable tool."""
        return {
            "name": self.name,
            "description": self.description,
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {
                        "type": "string",
                        "description": "The shell command to run inside the sandbox.",
                    }
                },
                "required": ["command"],
            },
        }

    def __call__(self, command: str) -> str:
        return self._run(command)


def make_sandbox_tool(sandbox: IronClawSandbox) -> LettaSandboxTool:
    """Wrap a live IronClaw sandbox as a Letta client-side ``sandboxed_shell`` tool.

    The sandbox handle is captured in a closure, so the returned object carries no
    extra state for Letta to serialize -- exactly what client-side execution
    wants: the server only ever sees the schema, never the executor.
    """

    def _run(command: str) -> str:
        return str(sandbox.run(command))

    return LettaSandboxTool(name="sandboxed_shell", description=_DESCRIPTION, _run=_run)
