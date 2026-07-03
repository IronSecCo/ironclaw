# Integration guides: launch social posts (IRO-297)

Short posts to convert launch traffic to the three framework integration guides
(LangChain, OpenAI SDK, CrewAI). Docs pages:

- `docs/integrations/langchain.md`
- `docs/integrations/openai-sdk.md`
- `docs/integrations/crewai.md`

**Status: DRAFT, launch-gated on IRO-40. Do not post before the launch gate.**

Guardrails applied: no owner name, no personal GitHub / LinkedIn / Instagram
handles; Reddit is in scope. No em-dashes or en-dashes (public-copy standing rule).
Every claim maps to a shipped, docs-linked capability.

Replace `<REPO>` with `https://github.com/IronSecCo/ironclaw` and `<DOCS>` with the
published docs base URL at post time.

---

## Hacker News (Show HN style, one post covering all three)

**Title:** Show HN: Run your LangChain / OpenAI-SDK / CrewAI agent behind a sealed sandbox

**Body:**

We kept seeing the same thing: people prototype an agent with LangChain, the OpenAI
SDK, or CrewAI, then run it somewhere real with the API key in the process, tools
executing with full local privileges, and wide-open egress. One prompt injection
and that is the box.

IronClaw is an AGPLv3 (plus commercial) alternative that runs the agent behind a
sealed sandbox instead: no network card, the model key held host-side and never in
the agent, and privileged actions (a new tool, a new agent, a new egress host)
routed through a human-approval gateway and an audit log. The sandbox has no
interpreter and no in-sandbox install, so you re-declare the agent (persona, model,
tools) rather than shipping code into it. That is the security guarantee, not a
limitation.

We wrote three short guides that map each framework onto IronClaw field by field,
plus a credential-free demo (offline mock provider, just Docker) so you can watch
the sealed loop run before porting anything:

- LangChain: <DOCS>/integrations/langchain/
- OpenAI SDK: <DOCS>/integrations/openai-sdk/
- CrewAI: <DOCS>/integrations/crewai/

Repo: <REPO>

Happy to answer questions about the isolation model.

---

## Reddit r/LangChain

**Title:** Running a LangChain agent behind a sealed sandbox (key held host-side, tools gated)

A LangChain `AgentExecutor` runs your tools in your own process, with your API key
in memory and open outbound network. That is fine for a prototype and risky the
moment it touches real inputs.

We wrote a short guide on porting a LangChain agent to IronClaw, an open-source
runtime that runs the same job behind a sealed sandbox: no NIC, the model key
injected host-side (never in the agent), and privileged tool calls routed through a
human-approval gateway. There is a field-by-field mapping (`ChatOpenAI` to a
provider, tools to built-ins or MCP, the system prompt to a persona) and a
credential-free mock demo so you can see it run first.

Guide: <DOCS>/integrations/langchain/
Repo (AGPLv3): <REPO>

---

## Reddit r/LocalLLaMA

**Title:** Sandbox your OpenAI-SDK / CrewAI agents and keep the model fully local

If you are running agents against a local model (Ollama, LM Studio, vLLM), IronClaw
keeps the whole stack on your box: the sandbox has no network card, and its only
egress is an audited proxy socket to the model you allowlist. No cloud key anywhere
in the stack.

New guides show how to port an OpenAI-SDK or CrewAI agent onto it, with the model
key (when you do use a hosted one) held host-side and never in the agent:

- OpenAI SDK: <DOCS>/integrations/openai-sdk/
- CrewAI: <DOCS>/integrations/crewai/
- Local model setup: <DOCS>/tutorials/local-model-ollama/

Repo: <REPO>

---

## Reddit r/selfhosted

**Title:** Self-host your agents behind a provable sandbox (LangChain / OpenAI SDK / CrewAI)

IronClaw is an open-source, self-hostable runtime for AI agents that runs each agent
in a per-session sandbox with no network card, host-side credentials, and a
human-approval gateway for anything privileged. One command brings up a
credential-free demo (offline mock provider, just Docker).

We just published guides for bringing agents you built with LangChain, the OpenAI
SDK, or CrewAI onto it:

<DOCS>/integrations/
Repo (AGPLv3 plus commercial): <REPO>

---

## X / Twitter (thread)

1/ You prototyped an agent with LangChain, the OpenAI SDK, or CrewAI.

Then it runs for real: API key in the process, tools with full local privileges,
egress wide open. One prompt injection = your box.

2/ IronClaw runs the same agent behind a sealed sandbox:

- no network card
- model key held host-side, never in the agent
- new tool / new agent / new egress host all gated on human approval + audit

3/ The sandbox has no interpreter and no in-sandbox install, so you re-declare the
agent (persona, model, tools) instead of shipping code into it. That is the
security guarantee.

4/ Three short guides, field by field, plus a credential-free demo (offline mock
provider, just Docker) so you can watch it run first:

LangChain / OpenAI SDK / CrewAI -> <DOCS>/integrations/

Open source (AGPLv3): <REPO>

---

## LinkedIn (company voice, no personal handle)

Most AI agent frameworks optimize for building behavior fast. Few give you a
security perimeter for when that agent runs against real inputs: the model key sits
in the process, tools execute with full local privileges, and outbound network is
open by default.

IronClaw is an open-source (AGPLv3 plus commercial) runtime that closes those gaps
by construction. Each agent runs in a sealed, per-session sandbox with no network
card, the model credential held host-side and injected on the way out, and every
privileged action routed through a human-approval gateway and an audit log.

We published three guides for teams already building with LangChain, the OpenAI SDK,
or CrewAI, each mapping the framework onto IronClaw field by field, with a
credential-free demo you can run in minutes.

Read them: <DOCS>/integrations/
