"""Haystack (deepset) tool backed by an IronClaw sandbox.

Drop-in replacement for any Haystack tool that shells out to the host (a custom
`Tool` wrapping `subprocess`, a code-interpreter component, ...): a Haystack
`Agent` / `ToolInvoker` calls it the same way, but every command runs inside an
isolated IronClaw per-session sandbox instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real haystack.tools.Tool
    # Agent(chat_generator=..., tools=[tool])  # ... drop into any Haystack agent

Requires haystack-ai >= 2.9 (the `haystack.tools.Tool` API graduated from
`haystack_experimental` in 2.9). The IronClaw sandbox client itself is pure
standard library.
"""

from __future__ import annotations

from haystack.tools import Tool

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this to run any code. The sandbox has no "
    "network, no host filesystem access, and no Docker socket, so untrusted "
    "commands are safe to run."
)

# JSON-schema for the tool's single argument. Haystack passes this straight to the
# chat model as the function-calling parameter schema.
_PARAMETERS = {
    "type": "object",
    "properties": {
        "command": {
            "type": "string",
            "description": "The shell command to run inside the sandbox.",
        }
    },
    "required": ["command"],
}


def make_sandbox_tool(sandbox: IronClawSandbox) -> Tool:
    """Wrap a live IronClaw sandbox as a Haystack `Tool`.

    The sandbox handle is captured in a closure, so the returned `Tool` carries no
    extra state beyond the standard Haystack fields and stays robust across
    haystack-ai versions.
    """

    def _run(command: str) -> str:
        return str(sandbox.run(command))

    return Tool(
        name="sandboxed_shell",
        description=_DESCRIPTION,
        parameters=_PARAMETERS,
        function=_run,
    )
