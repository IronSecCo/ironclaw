#!/usr/bin/env python3
"""Run a LangGraph agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the tool
through LangGraph's own `ToolNode` -- the exact node a compiled LangGraph agent
uses to run tool calls -- over one benign task plus a battery of escape attempts,
then prints a PASS/FAIL containment table. No LLM key needed: we hand the
`ToolNode` the tool-call messages an LLM would have emitted.

Set OPENAI_API_KEY to instead let a real LLM-driven `create_react_agent` graph
decide what to run; the tool (and therefore the isolation) is identical either
way.
"""

from __future__ import annotations

import os
import sys

# Make the shared client + demo importable (examples/integrations/_shared).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "_shared"))

from containment_demo import PROBES, run_containment_demo  # noqa: E402
from ironclaw_sandbox import IronClawSandbox  # noqa: E402
from ironclaw_tool import make_sandbox_tool  # noqa: E402


def drive_with_real_agent(tool) -> int:
    """Optional: let a real LLM-driven ReAct graph decide what to run (needs a key)."""
    from langchain_openai import ChatOpenAI
    from langgraph.prebuilt import create_react_agent

    llm = ChatOpenAI(model="gpt-4o-mini", temperature=0)
    agent = create_react_agent(llm, [tool])
    result = agent.invoke(
        {
            "messages": [
                ("user", "Run `id` and tell me which user the sandbox runs as.")
            ]
        }
    )
    print(result["messages"][-1].content)
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real create_react_agent graph")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: compile a real LangGraph StateGraph with
        # a ToolNode -- the exact node a compiled agent runs tool calls with -- and
        # drive it per probe. We feed the graph the AIMessage tool-call an LLM node
        # would emit and read the ToolMessage the graph appends. No LLM key needed.
        from langchain_core.messages import AIMessage
        from langgraph.graph import END, START, MessagesState, StateGraph
        from langgraph.prebuilt import ToolNode

        graph = StateGraph(MessagesState)
        graph.add_node("tools", ToolNode([tool]))
        graph.add_edge(START, "tools")
        graph.add_edge("tools", END)
        app = graph.compile()
        print(f"==> driving a LangGraph StateGraph over {len(PROBES)} agent tool calls")

        def invoke(command: str) -> str:
            call = AIMessage(
                content="",
                tool_calls=[
                    {"name": tool.name, "args": {"command": command}, "id": "probe"}
                ],
            )
            result = app.invoke({"messages": [call]})
            return result["messages"][-1].content

        return run_containment_demo(invoke, framework="LangGraph")


if __name__ == "__main__":
    raise SystemExit(main())
