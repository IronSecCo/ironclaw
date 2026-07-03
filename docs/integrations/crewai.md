---
title: "From CrewAI to a sandboxed IronClaw agent"
description: You built a multi-agent crew with CrewAI. Here is how to run those agents behind IronClaw's sealed sandbox, with model keys held host-side, agent-to-agent messaging host-mediated, and spawning a new agent gated on human approval, plus a credential-free way to see it work first.
---

# From CrewAI to a sandboxed IronClaw agent

You built a crew with **CrewAI**: several agents, each with a role and tools,
handing work to one another. Multi-agent is where the blast radius grows. Every
agent in the crew runs in your process with your keys, every tool runs with your
privileges, and agents talk to each other in-process with no boundary between them.
A single poisoned task can travel the whole crew.

IronClaw runs the same crew behind **sealed, per-session sandboxes**: no network
card, model keys held **host-side**, **agent-to-agent messaging host-mediated and
audited** (agents never talk peer-to-peer), and **spawning a new agent gated on
mandatory human approval** because a new agent is a new trust principal.

!!! info "IronClaw does not run your Python in the sandbox — and that is the point"
    IronClaw's sandbox has **no interpreter and no in-sandbox install**. You do not
    *wrap* the CrewAI process; you re-declare each crew member (role, model, tools)
    as an IronClaw agent group, and IronClaw runs them inside the sealed runtime.
    See [Skills](../skills.md) and [Security and isolation](../security-isolation.md).

## Why sandbox this

A typical CrewAI crew:

```python
from crewai import Agent, Crew, Task

researcher = Agent(role="Researcher", tools=[search_tool], llm=llm)   # key in-process
writer     = Agent(role="Writer",     tools=[file_tool],  llm=llm)
crew = Crew(agents=[researcher, writer], tasks=[Task(...)])
crew.kickoff()   # agents run in-process, hand off in-process, tools run on your box
```

The multi-agent shape multiplies the exposure:

1. **Every agent holds the keys.** More agents, more places `sk-...` lives.
2. **Tools run with your privileges.** `search_tool`, `file_tool`, and any shell or
   HTTP tool execute on your host. Any crew member is a path in.
3. **Hand-offs have no boundary.** Agents pass work in-process; a compromised
   researcher can steer the writer with no gate between them.

IronClaw puts a boundary at each of those seams.

## See it work first (no credentials)

Watch the sealed loop run with the offline `mock` provider, no key required:

```sh
git clone https://github.com/IronSecCo/ironclaw.git && cd ironclaw
docker compose -f docker-compose.demo.yml up --build -d      # start the demo control-plane

curl -s -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H 'authorization: Bearer ironclaw-demo' -H 'content-type: application/json' \
  -d '{"agentGroupID":"mock-agent","text":"hello from the crew"}'

sleep 3
curl -s -H 'authorization: Bearer ironclaw-demo' \
  http://127.0.0.1:8787/v1/ui/chat/mock-agent/messages       # the reply
```

The reply comes back through a real per-session sandbox and encrypted queues. Tear
down with `docker compose -f docker-compose.demo.yml down`. For a two-agent shape
close to a crew, see
[`examples/multi-agent-team`](https://github.com/IronSecCo/ironclaw/tree/main/examples/multi-agent-team).

## Port your crew

Each CrewAI `Agent` becomes an IronClaw agent group:

| CrewAI | IronClaw | Notes |
|---|---|---|
| `Agent(role=..., llm=...)` | one `ironctl agent create` per member | Each crew member is its own sandboxed agent group. |
| `llm=ChatOpenAI(api_key=...)` | `--provider openai` + host `OPENAI_API_KEY` | Keys are host-side and injected by the model-proxy. No sandbox ever holds one. |
| `role` / `backstory` / `goal` | `--identity` / `--soul` / `--instructions` | The agent's identity, voice, and operating rules. |
| `tools=[...]` | `--tool <name>` (built-in) or MCP | Built-ins: `read_file`, `write_file`, `list_dir`, `web_search`, `http_fetch`. Custom tools attach over [MCP](../mcp.md). |
| Agent-to-agent hand-off | host-mediated a2a messaging | The control-plane routes and audits every hand-off; agents never talk peer-to-peer. |
| `Crew` spawning an agent | `create_agent` behind the approval gateway | Spawning a new agent is **never auto-approved** (threat-model boundary B3). |

A researcher-plus-writer crew becomes two declared agents:

```sh
export OPENAI_API_KEY=sk-...          # host-side only; no sandbox sees it
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
./bin/controlplane --dev --api-addr 127.0.0.1:8787 &

ironctl agent create --name "Researcher" \
  --provider openai --model gpt-4o \
  --instructions "You research the topic and cite sources." \
  --tool web_search --tool http_fetch --yes

ironctl agent create --name "Writer" \
  --provider openai --model gpt-4o \
  --instructions "You turn research notes into a short report." \
  --tool read_file --tool write_file --yes
```

The [`multi-agent-team`](https://github.com/IronSecCo/ironclaw/tree/main/examples/multi-agent-team)
example wires two agents into one shared channel with priority and engage rules,
so you can see mediated hand-off end to end.

## What you gained

- **Keys left every agent.** Injected host-side per request; no crew member holds a
  secret to leak.
- **`network=none` by default.** Each agent runs with no NIC; egress is the audited
  model-proxy socket plus explicitly allowlisted hosts.
- **Hand-offs and spawns are gated.** Agent-to-agent messages are host-routed and
  audited, and `create_agent` always requires human approval. See the
  [threat model](../threat-model.md) (boundary B3).

The crew you designed in CrewAI, with a boundary at every seam it did not have.

## Next

- [Choose your model provider](../providers/index.md)
- [MCP: bring your own tools](../mcp.md)
- [multi-agent-team example](https://github.com/IronSecCo/ironclaw/tree/main/examples/multi-agent-team)
