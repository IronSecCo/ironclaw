"""DSPy tool backed by an IronClaw sandbox.

Drop-in replacement for the host-executing shell/code tool you would otherwise
hand a **DSPy** (Stanford) program (a plain Python function wrapped as
`dspy.Tool`, or one passed to a `dspy.ReAct` module): the program calls it
exactly the same way, but every command runs inside an isolated IronClaw
per-session sandbox instead of on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    tool = make_sandbox_tool(sandbox)          # a real dspy.Tool
    program = dspy.ReAct("task -> answer", tools=[tool])
"""

from __future__ import annotations

import dspy

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> dspy.Tool:
    """Wrap a live IronClaw sandbox as a dspy.Tool named ``sandboxed_shell``.

    The sandbox handle is captured in a closure, so the returned object is a
    plain ``dspy.Tool`` with no extra state for DSPy to introspect. DSPy calls a
    tool as ``tool(command=...)`` (and infers the arg schema from the wrapped
    function's signature, type hints, and docstring), which is exactly how the
    containment demo drives it below.
    """

    def sandboxed_shell(command: str) -> str:
        """Run a shell command inside the IronClaw sandbox and return its output.

        Args:
            command: The shell command to run inside the sandbox.
        """
        return str(sandbox.run(command))

    return dspy.Tool(sandboxed_shell, name="sandboxed_shell", desc=_DESCRIPTION)
