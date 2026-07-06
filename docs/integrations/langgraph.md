---
title: "Sandbox your LangGraph agent with IronClaw"
description: How to sandbox a LangGraph agent. Run your LangGraph graph's untrusted tool and code execution inside IronClaw's sealed gVisor sandbox, with the model key held host-side and every tool call gated and audited, plus a credential-free way to see it work first.
---

# Sandbox your LangGraph agent with IronClaw

You built an agent with **LangGraph**: a stateful graph of nodes, where a `ToolNode`
(or the prebuilt `create_react_agent`) executes the tool calls the model emits. That
is a great way to design *control flow*. The problem starts when it runs somewhere
real: that `ToolNode` runs your tools **in your own process**, with your API key in
memory and unrestricted outbound network. One prompt-injected instruction and every
edge in your graph is an edge on your host.

IronClaw runs the same job behind a **sealed sandbox** instead: no network card, the
model key held **host-side** and never in the agent, and every privileged tool call
routed through a human-approval gateway and an audit log.

!!! example "Runnable example"
    A one-command LangGraph-to-IronClaw example lives at
    [`examples/integrations/langgraph`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/langgraph):
    it compiles a real LangGraph `StateGraph` with a `ToolNode` whose
    `sandboxed_shell` tool executes inside an IronClaw sandbox, then prints a
    PASS/FAIL containment table with every escape attempt blocked. Zero credentials,
    just Docker.

## Why sandbox this

A typical LangGraph agent wires a `ToolNode` straight to host execution:

```python
from langgraph.prebuilt import ToolNode, create_react_agent
from langchain_community.tools import ShellTool
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(model="gpt-4o", api_key="sk-...")   # key lives in this process
agent = create_react_agent(llm, [ShellTool()])        # ShellTool runs on YOUR box
agent.invoke({"messages": [("user", "summarize today's incidents")]})
```

Three things are true of that graph, and all three are risks:

1. **The key is in the process.** Anything that can read `os.environ` can read `sk-...`.
2. **Every tool node runs with your privileges.** `ShellTool` executes on your host,
   with your filesystem and network, on any path the graph can reach.
3. **Egress is wide open.** The process can reach any host on the internet.

IronClaw closes all three by construction. The
[example](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/langgraph)
keeps your graph exactly as designed — the same `ToolNode` — but points its
execution at a sealed per-session sandbox.

## See it work first (no credentials)

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
examples/integrations/langgraph/run.sh
```

It brings up the offline `mock` provider (no model key), compiles a LangGraph
`StateGraph` with a `ToolNode`, and drives it over one benign task plus a battery of
escape attempts — network exfil, host-filesystem read, Docker-socket takeover — each
reported **BLOCKED**. The run exits non-zero if any containment expectation fails, so
it doubles as a CI check.

## The fix, in your graph

Keep the graph; swap the tool. The example's `make_sandbox_tool(sandbox)` returns an
ordinary LangChain `StructuredTool`, so it drops into a `ToolNode` or
`create_react_agent` with no other change:

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool
from langgraph.prebuilt import ToolNode

with IronClawSandbox() as sandbox:
    tool = make_sandbox_tool(sandbox)     # runs inside the sealed sandbox, not your host
    tool_node = ToolNode([tool])          # ... the exact node your graph already uses
```

Every command the model routes through that node now executes inside the box as an
unprivileged uid, with `network=none` and no host mounts.

## What you gained

- **The key left the agent.** It lives host-side and is injected per request; a
  compromised graph has nothing to steal.
- **`network=none` by default.** The sandbox has no NIC. The only egress is the
  audited model-proxy socket, plus whatever hosts you explicitly allowlist.
- **Every tool node is contained.** No matter which path your graph takes, the tool
  call lands in the sandbox, and privileged actions flow through a human-approval
  gateway into the [audit log](../architecture.md).

Same stateful graph you designed in LangGraph. A perimeter it never had.

## Next

- [Sandbox your LangChain agent](langchain.md)
- [Choose your model provider](../providers/index.md)
- [Security and isolation](../security-isolation.md)
