# IronClaw examples

Runnable templates that configure a real IronClaw agent against a running
control-plane. Each one is a directory with a `README.md` (what it does and how
to try it) and a `setup.sh` (the exact `ironctl` commands, idempotent where the
API allows).

### Start here: prove it works in one command

[**`hello-ironclaw/`**](hello-ironclaw/) is the canonical zero-credential
end-to-end check. One command builds the sandbox image, brings up the offline demo
control-plane, sends a chat through the **real** secured path (engage → per-session
Docker sandbox → encrypted queue → delivery), and **asserts** the reply comes back —
then tears it down. It exits non-zero if the round-trip breaks, so it also doubles as
IronClaw's user-facing CI smoke test
([`example-smoke.yml`](../.github/workflows/example-smoke.yml)):

```sh
examples/hello-ironclaw/run.sh        # build → up → assert reply → tear down
```

### Prove the sandbox holds (adversarial harness)

[**`red-team-escape/`**](red-team-escape/) is the other side of the coin: instead of
proving the happy path *works*, it tries to **break** the sandbox and proves the
attacks are contained. It engages a real per-session sandbox and then, assuming a
fully-jailbroken agent with arbitrary code execution inside the box (simulated with
`docker exec`), runs a battery of escape / exfiltration / self-modification attempts —
network egress, host escape via the Docker socket, sibling breakout, and gateway-held
self-modification — and emits a PASS/FAIL table, exiting non-zero if any core
containment assertion fails. Same zero-credential path, no model key:

```sh
examples/red-team-escape/run.sh       # build → up → attack → assert contained → tear down
```

### Run the whole matrix in one command (release-readiness gate)

[**`smoke-matrix.sh`**](smoke-matrix.sh) is the single release-readiness gate: it
brings up one offline demo control-plane and runs **every** example directory
end-to-end against it — the `hello-ironclaw` / `red-team-escape` round-trips, the
three `run-mock.sh` reply recipes (each asserting a non-empty `.content` reply, the
IRO-279 guard), and the four `setup.sh` config recipes (each asserting a real
messaging-group id is minted) — then prints a PASS/FAIL/SKIP table and exits
non-zero if any example produced empty or incorrect output. It rebuilds the demo
image from the current checkout first, so it also catches control-plane regressions
a stale image would hide. Zero credentials (mock provider); needs Docker + Go + jq:

```sh
make smoke                 # build images → up → run every example → assert → tear down
examples/smoke-matrix.sh --attach   # against an already-running demo control-plane
```

It runs in CI as the `smoke-matrix` job in
[`example-smoke.yml`](../.github/workflows/example-smoke.yml).

### Bring your agent SDK, run its tools in the sandbox

[**`integrations/`**](integrations/) ports agents built with popular SDKs so their
**code/tool execution runs inside a sealed IronClaw sandbox** — no network card, no host
filesystem, no Docker socket — while the SDK still plans the calls. Each runs
credential-free by default and ends by printing real escape attempts **BLOCKED**:

```sh
examples/integrations/openai-agents/run.sh        # OpenAI Agents SDK
examples/integrations/claude-agent-sdk/run.sh     # Claude Agent SDK
```

### Run a sandboxed agent in CI (GitHub Action)

[**`ci-action/`**](ci-action/) documents the reusable
[`IronClaw` action](../.github/actions/ironclaw): a workflow step that runs a
one-shot sandboxed agent against a `prompt` credential-free (the `mock` provider),
returns the reply, and — with `containment-report: true` — freezes the signed-able
isolation proof for the build under test. It is a thin wrapper over the same
zero-credential path above, proven in this repo by
[`ironclaw-action-example.yml`](../.github/workflows/ironclaw-action-example.yml).

### Run a scenario end-to-end credential-free (mock provider)

Three recipes ship a `run-mock.sh` that exercises the **whole** inbound → agent →
reply pipeline against the offline `mock` provider — **no model key, no channel
tokens**. Bring up the zero-credential demo control-plane once, then run any of
them from the repo root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # seeds the offline mock-agent
./examples/scheduled-report/run-mock.sh
./examples/webhook-responder/run-mock.sh
./examples/slack-triage/run-mock.sh
docker compose -f docker-compose.demo.yml down            # tear down
```

| Recipe | What it shows | Credential-free demo |
|--------|---------------|:--------------------:|
| [`hello-ironclaw/`](hello-ironclaw/) | The full path working end-to-end, asserted — the smoke test + first "it works". | ✅ `run.sh` (self-contained) |
| [`red-team-escape/`](red-team-escape/) | The sandbox **holding** under attack — network egress, host/socket escape, sibling breakout, and gateway-held self-modification, all asserted contained. | ✅ `run.sh` (self-contained) |
| [`integrations/openai-agents/`](integrations/openai-agents/) | An **OpenAI Agents SDK** agent whose code-execution tool runs inside the sandbox; benign command works, prompt-injected escape **BLOCKED**. | ✅ `run.sh` (self-contained) |
| [`integrations/claude-agent-sdk/`](integrations/claude-agent-sdk/) | A **Claude Agent SDK** agent whose bash tool is backed by a sandbox session; benign command works, prompt-injected escape **BLOCKED**. | ✅ `run.sh` (self-contained) |
| [`ollama/`](ollama/) | A real agent on a **local Ollama model** with **zero cloud API key** — the first-class `ollama` provider, create-group + chat + reply. | ✅ `setup.sh` (needs local Ollama) |
| [`scheduled-report/`](scheduled-report/) | An agent that wakes itself on a schedule (`schedule_task`), summarizes, and posts to a channel. | ✅ `run-mock.sh` |
| [`webhook-responder/`](webhook-responder/) | An inbound HTTP webhook routed to an agent that replies (poll or push-back via a `webhook` destination). | ✅ `run-mock.sh` |
| [`slack-triage/`](slack-triage/) | A bot that classifies/labels **every** incoming Slack message. | ✅ `run-mock.sh` |
| [`personal-assistant/`](personal-assistant/) | A private 1:1 assistant on Telegram that replies to every message — plus the mandatory change-approval flow. | |
| [`channel-triage/`](channel-triage/) | A triage bot in a shared Slack channel: engages only on `@mention`, only for known senders, and accumulates context from the messages it ignores. | |
| [`multi-agent-team/`](multi-agent-team/) | Two agents wired into one group chat (a frontline responder + a scribe), showing priorities, multi-agent wiring, and where agent-to-agent / `create_agent` sits. | |
| [`keyword-watcher/`](keyword-watcher/) | A quiet ops agent in a Discord channel that engages only on a `pattern` match (`deploy`/`incident`/`outage`), from any sender, one session per incident thread. | |

### Framework integrations

Already using LangChain or CrewAI? [**`integrations/`**](integrations/) backs
your agent's untrusted code execution with a real IronClaw sandbox instead of a
host shell — the same tool interface, none of the host risk. Each ships a
one-command, zero-credential containment demo:

| Integration | What it shows | Credential-free demo |
|-------------|---------------|:--------------------:|
| [`integrations/langchain/`](integrations/langchain/) | A LangChain `StructuredTool` (`sandboxed_shell`) whose commands run inside an IronClaw per-session sandbox; benign code runs, every escape attempt is contained. | ✅ `run.sh` (self-contained) |
| [`integrations/crewai/`](integrations/crewai/) | The same pattern as a CrewAI `BaseTool` for a crew agent. | ✅ `run.sh` (self-contained) |

## Prerequisites

1. A running control-plane. For a local trial, dev mode is enough:

   ```sh
   export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
   ironclaw-controlplane --dev --api-addr 127.0.0.1:8787 &
   ```

2. The two env vars every template reads:

   - `IRONCLAW_API_TOKEN` — the control-plane API token (required).
   - `IRONCLAW_ADDR` — the API base URL (optional; defaults to
     `http://127.0.0.1:8787`).

3. [`jq`](https://jqlang.github.io/jq/) — the scripts read server-assigned ids
   (e.g. a messaging-group id) out of the JSON responses with it.

## Running a template

```sh
cd examples/channel-triage
./setup.sh
```

Then verify what was created:

```sh
ironctl registry session list           # active sessions
ironctl audit --limit 20                 # the append-only gateway audit log
```

> These templates configure the **control-plane** (agent groups, channels,
> wirings, access). The agent's actual persona/tooling content is applied
> through the gateway's human-approval flow — `personal-assistant/` walks through
> that. Identifiers in the scripts (channel ids, phone numbers, user handles) are
> placeholders; edit them for your own setup.
