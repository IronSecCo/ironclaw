"""Semantic Kernel plugin backed by an IronClaw sandbox.

Drop-in replacement for the host-executing code/shell function you would normally
expose to a **Semantic Kernel** agent (a native `@kernel_function` that shells
out, a code-interpreter plugin, ...): the kernel calls it exactly the same way,
but every command runs inside an isolated IronClaw per-session sandbox instead of
on your machine.

    from ironclaw_tool import make_sandbox_tool
    from ironclaw_sandbox import IronClawSandbox

    sandbox = IronClawSandbox().__enter__()
    plugin = make_sandbox_tool(sandbox)        # a real SK native plugin
    kernel.add_plugin(plugin, plugin_name="ironclaw")
"""

from __future__ import annotations

from semantic_kernel.functions import kernel_function

from ironclaw_sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


def make_sandbox_tool(sandbox: IronClawSandbox) -> object:
    """Wrap a live IronClaw sandbox as a Semantic Kernel native plugin.

    The sandbox handle is captured in a closure so it does not have to be a
    pydantic/kernel field on the plugin, which keeps this robust across
    semantic-kernel versions.
    """

    class IronClawSandboxPlugin:
        @kernel_function(name="sandboxed_shell", description=_DESCRIPTION)
        def sandboxed_shell(self, command: str) -> str:
            """Run a shell command inside the IronClaw sandbox and return its output."""
            return str(sandbox.run(command))

    return IronClawSandboxPlugin()
