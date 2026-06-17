# IronClaw examples

Runnable templates that configure a real IronClaw agent against a running
control-plane. Each one is a directory with a `README.md` (what it does and how
to try it) and a `setup.sh` (the exact `ironctl` commands, idempotent where the
API allows).

| Template | What it shows |
|----------|---------------|
| [`personal-assistant/`](personal-assistant/) | A private 1:1 assistant on Telegram that replies to every message — plus the mandatory change-approval flow. |
| [`channel-triage/`](channel-triage/) | A triage bot in a shared Slack channel: engages only on `@mention`, only for known senders, and accumulates context from the messages it ignores. |
| [`multi-agent-team/`](multi-agent-team/) | Two agents wired into one group chat (a frontline responder + a scribe), showing priorities, multi-agent wiring, and where agent-to-agent / `create_agent` sits. |
| [`keyword-watcher/`](keyword-watcher/) | A quiet ops agent in a Discord channel that engages only on a `pattern` match (`deploy`/`incident`/`outage`), from any sender, one session per incident thread. |

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
