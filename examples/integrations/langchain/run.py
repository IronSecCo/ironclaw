#!/usr/bin/env python3
"""Run a LangChain agent whose code execution is backed by an IronClaw sandbox.

Zero credentials by default: it engages a real IronClaw per-session sandbox
against the offline demo control-plane (mock provider) and drives the LangChain
`sandboxed_shell` tool exactly as an agent would -- one benign task plus a
battery of escape attempts -- then prints a PASS/FAIL containment table.

Set OPENAI_API_KEY to instead let a real LLM-driven ReAct agent decide what to
run; the tool (and therefore the isolation) is identical either way.
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
    """Optional: let a real LLM-driven agent decide what to run (needs a key)."""
    from langchain.agents import AgentExecutor, create_react_agent
    from langchain_core.prompts import PromptTemplate
    from langchain_openai import ChatOpenAI

    prompt = PromptTemplate.from_template(
        "You are a helpful assistant with a `sandboxed_shell` tool.\n"
        "Answer the question by running commands.\n\n"
        "{tools}\nTool names: {tool_names}\n\n"
        "Use this format:\nQuestion: {input}\nThought: ...\n"
        "Action: the tool name\nAction Input: the command\n"
        "Observation: the result\n... (repeat)\nFinal Answer: ...\n\n"
        "{agent_scratchpad}"
    )
    llm = ChatOpenAI(model="gpt-4o-mini", temperature=0)
    agent = create_react_agent(llm, [tool], prompt)
    executor = AgentExecutor(agent=agent, tools=[tool], verbose=True)
    result = executor.invoke(
        {"input": "Run `id` and tell me which user the sandbox runs as."}
    )
    print(result["output"])
    return 0


def main() -> int:
    print("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)")
    with IronClawSandbox() as sandbox:
        print(f"    sandbox container: {sandbox.container}")
        tool = make_sandbox_tool(sandbox)

        if os.environ.get("OPENAI_API_KEY"):
            print("==> OPENAI_API_KEY set: driving a real ReAct agent")
            return drive_with_real_agent(tool)

        # Deterministic, zero-cred path: call the tool exactly as an AgentExecutor
        # does -- tool.invoke({"command": ...}) -- for each probe.
        print(f"==> driving the '{tool.name}' tool over {len(PROBES)} agent commands")
        return run_containment_demo(
            lambda command: tool.invoke({"command": command}),
            framework="LangChain",
        )


if __name__ == "__main__":
    raise SystemExit(main())
