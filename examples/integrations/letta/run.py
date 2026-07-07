#!/usr/bin/env python3
"""Run a Letta (MemGPT) agent whose tool execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the Letta
`sandboxed_shell` client-side tool exactly as an agent would when the model picks
it -- one benign task plus a battery of escape attempts -- then prints a
PASS/FAIL containment table.

Set LETTA_BASE_URL (a running Letta server, e.g. http://localhost:8283) to
instead let a real LLM-driven Letta agent decide what to run via client-side tool
execution; the executor -- and therefore the isolation -- is identical either way.
"""

from __future__ import annotations

import json
import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def drive_with_real_agent(tool) -> int:
    """Optional: a real LLM-driven Letta agent, executing the tool client-side.

    Letta client-side tools run in *this* process: the agent emits an approval /
    tool-call request, we execute it against the live IronClaw sandbox, and post
    the result back. Requires a running Letta server (LETTA_BASE_URL) with a model
    configured. Needs `pip install letta-client`.
    """
    from letta_client import Letta

    client = Letta(base_url=os.environ["LETTA_BASE_URL"])
    agent = client.agents.create(
        name="ironclaw-sandboxed-agent",
        memory_blocks=[
            {"label": "persona", "value": "You run everything inside an isolated "
             "IronClaw sandbox via the sandboxed_shell tool. Never assume a "
             "command's output; run it."},
        ],
    )
    client_tools = [tool.schema]
    response = client.agents.messages.create(
        agent_id=agent.id,
        messages=[{"role": "user", "content": "Run `id` with the sandboxed_shell "
                   "tool and tell me which user the sandbox runs as."}],
        client_tools=client_tools,
    )
    # Service any client-side tool calls the model made, returning results until
    # the agent stops requesting tools.
    while True:
        pending = [m for m in response.messages
                   if getattr(m, "message_type", None) == "approval_request_message"]
        if not pending:
            break
        approvals = []
        for msg in pending:
            call = msg.tool_call
            args = json.loads(call.arguments)
            output = tool(command=args["command"])
            approvals.append({
                "type": "tool",
                "tool_call_id": call.tool_call_id,
                "tool_return": output,
                "status": "success",
            })
        response = client.agents.messages.create(
            agent_id=agent.id,
            messages=[{"type": "approval", "approvals": approvals}],
            client_tools=client_tools,
        )
    for msg in response.messages:
        text = getattr(msg, "content", None) or getattr(msg, "text", None)
        if text:
            print(text)
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("LETTA_BASE_URL"):
            print("==> LETTA_BASE_URL set: driving a real Letta agent (client-side tools)")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: dispatch the tool exactly as a Letta
        # client-side tool call does -- tool(command=...) -- per probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: tool(command=command),
            framework="Letta",
        )


if __name__ == "__main__":
    raise SystemExit(main())
