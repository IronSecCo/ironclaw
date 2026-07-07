---
title: "Sandbox any AI agent framework with IronClaw"
description: You built an agent with LangChain, LangGraph, CrewAI, Agno, LlamaIndex, Haystack, Pydantic AI, AutoGen, Semantic Kernel, the OpenAI Agents SDK, the Claude Agent SDK, the Vercel AI SDK, LangChain.js, Mastra, Hugging Face smolagents, or Google ADK. Those frameworks run your agent's tools and code in your own process, on your host. IronClaw runs the same agent inside a sealed gVisor sandbox instead. One search-intent guide per framework.
---

# Sandbox any AI agent framework with IronClaw

Every agent framework is great at designing *behavior*: a persona, a model, and a
set of tools the model can call. None of them was built to answer the question that
matters the moment the agent runs somewhere real:

**When your agent runs untrusted, model-chosen code, whose machine is it running on?**

With LangChain, LangGraph, CrewAI, Agno, LlamaIndex, Haystack, Pydantic AI, AutoGen, Semantic
Kernel, the OpenAI Agents SDK, the Claude Agent SDK, the Vercel AI SDK, LangChain.js,
Mastra, Hugging Face smolagents, Google ADK, and DSPy, the answer is the same: **yours**. The tool loop runs in your process, with your API key in
memory, your filesystem, and unrestricted outbound network. A single prompt
injection inside a document the agent reads can turn a `Bash` or `ShellTool` call
into a shell on your box.

IronClaw runs the same agent behind a **sealed, per-session sandbox** instead:

- **No network card.** The sandbox runs `network=none` by default. The only egress
  is an audited model-proxy socket plus hosts you explicitly allowlist.
- **The key never enters the agent.** It is held host-side and injected per request
  by the model-proxy. A compromised agent has nothing to steal.
- **Every privileged action is gated.** Registering a tool, spawning an agent, or
  reaching a new host flows through a human-approval gateway and lands in the
  [audit log](../architecture.md).

You do not wrap your framework's process. You re-declare the same agent (persona,
model, tools) to IronClaw, and it runs inside the sealed runtime. Same behavior you
designed, the perimeter you did not have to build. See how the sandbox holds under
attack in [Isolation, proven](../security-isolation.md) and the
[threat model](../threat-model.md).

## Pick your framework

| You built your agent with | Guide |
|---|---|
| **LangChain** | [Sandbox your LangChain agent](langchain.md) |
| **LangGraph** | [Sandbox your LangGraph agent](langgraph.md) |
| **CrewAI** | [Sandbox your CrewAI agents](crewai.md) |
| **Agno** (ex-Phidata) | [Sandbox your Agno agent](agno.md) |
| **LlamaIndex** | [Sandbox your LlamaIndex agent](llamaindex.md) |
| **Haystack** (deepset) | [Sandbox your Haystack agent](haystack.md) |
| **Pydantic AI** | [Sandbox your Pydantic AI agent](pydantic-ai.md) |
| **AutoGen** | [Sandbox your AutoGen agent](autogen.md) |
| **Semantic Kernel** | [Sandbox your Semantic Kernel agent](semantic-kernel.md) |
| **OpenAI Agents SDK** | [Sandbox your OpenAI Agents SDK agent](openai-sdk.md) |
| **Claude Agent SDK** | [Sandbox your Claude Agent SDK agent](claude-sdk.md) |
| **Vercel AI SDK** (JS/TS) | [Sandbox your Vercel AI SDK agent](vercel-ai-sdk.md) |
| **LangChain.js** (JS/TS) | [Sandbox your LangChain.js agent](langchain-js.md) |
| **Mastra** (JS/TS) | [Sandbox your Mastra agent](mastra.md) |
| **smolagents** (Hugging Face) | [Sandbox your smolagents agent](smolagents.md) |
| **Google ADK** | [Sandbox your Google ADK agent](google-adk.md) |
| **DSPy** (Stanford) | [Sandbox your DSPy agent](dspy.md) |
| **A CI pipeline** | [Run IronClaw in GitHub Actions](ci.md) |

Each guide covers the same three beats: the problem in your framework's own code,
the three-line fix, and a runnable example that prints a blocked escape attempt.

## See it work first (no credentials)

Every guide starts with a credential-free demo you can run in a minute with just
Docker, no model key:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages
```

The self-checking, one-command version is
[`examples/hello-ironclaw`](https://github.com/IronSecCo/ironclaw/tree/main/examples/hello-ironclaw).

## Go deeper

- [Why we run AI agents in gVisor](../gvisor-deep-dive.md) — the security model behind
  every guide above: no network card, no host key, no self-reconfiguration.
- [Bring your own model](../bring-your-own-model.md) — run any of these frameworks against
  a local (Ollama), Gemini, or Vertex model without a credential ever entering the sandbox.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [Learn: how to sandbox an AI agent](../learn/index.md)
