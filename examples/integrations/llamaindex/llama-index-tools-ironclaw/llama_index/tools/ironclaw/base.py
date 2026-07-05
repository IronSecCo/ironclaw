"""IronClaw sandbox tool spec for LlamaIndex.

``IronClawToolSpec`` exposes a single ``sandboxed_shell`` tool that runs a shell
command inside an isolated IronClaw per-session sandbox instead of on the host.
It is a drop-in replacement for LlamaIndex's host-executing code/shell tools: a
LlamaIndex agent calls it exactly the same way, but every command runs with
**no network, no host filesystem, and no Docker socket** -- the isolation
boundary IronClaw proves holds, not just promises.

    from llama_index.tools.ironclaw import IronClawToolSpec

    tool_spec = IronClawToolSpec()                 # points at the demo control-plane
    agent = FunctionAgent(tools=tool_spec.to_tool_list(), llm=llm)

See https://github.com/IronSecCo/ironclaw (examples/integrations/llamaindex) for
the runnable, zero-credential demo this package is extracted from.
"""

from __future__ import annotations

from typing import Optional

from llama_index.core.tools.tool_spec.base import BaseToolSpec

from llama_index.tools.ironclaw._sandbox import IronClawSandbox

_DESCRIPTION = (
    "Execute a shell command inside an isolated IronClaw sandbox and return its "
    "stdout/stderr and exit code. Use this for any code the user asks you to run. "
    "The sandbox has no network, no access to the host filesystem, and no Docker "
    "socket, so it is safe to run untrusted commands."
)


class IronClawToolSpec(BaseToolSpec):
    """Tool spec that backs a LlamaIndex agent's code execution with an IronClaw sandbox."""

    spec_functions = ["sandboxed_shell"]

    def __init__(
        self,
        addr: str = "http://127.0.0.1:8787",
        token: str = "ironclaw-demo",
        agent: str = "mock-agent",
        sandbox: Optional[IronClawSandbox] = None,
    ) -> None:
        """Initialize the tool spec.

        Args:
            addr: Base URL of the IronClaw control-plane. Defaults to the local
                offline demo control-plane.
            token: Bearer token for the control-plane. Defaults to the demo token.
            agent: Agent group ID whose per-session sandbox to engage.
            sandbox: An already-constructed :class:`IronClawSandbox` to reuse. If
                given, ``addr``/``token``/``agent`` are ignored.
        """
        self.sandbox: IronClawSandbox = sandbox or IronClawSandbox(
            addr=addr, token=token, agent=agent
        )

    def sandboxed_shell(self, command: str) -> str:
        """
        Run a shell command inside the isolated IronClaw sandbox.

        The sandbox is engaged lazily on first call and reused across calls. The
        command runs as the sandbox's own non-root uid with no network, no host
        filesystem, and no Docker socket. A non-zero exit is returned as data
        (never raised), so a contained attack is observable output.

        Args:
            command: The shell command to execute inside the sandbox.

        Returns:
            The combined stdout/stderr prefixed with the exit code, e.g.
            ``"[exit 0]\\nhello"``.
        """
        return str(self.sandbox.run(command))
