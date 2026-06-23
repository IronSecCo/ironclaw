---
title: Connect IronClaw to Slack
description: Wire an agent group to a live Slack channel — bot token, messaging group, wiring, and a reply destination.
---

# Connect IronClaw to Slack

This tutorial connects an IronClaw agent to a **real Slack channel**. You'll create a Slack bot,
hand its token to the control-plane, and use `ironctl` to wire an agent group so it engages in the
channel and posts back into it.

By the end you'll have a `triage` agent that:

- engages **only when `@mentioned`**,
- acts **only for known senders**,
- **accumulates** the messages it stays out of so it has context when called in, and
- can **post replies back** into the channel.

This mirrors the runnable [`examples/channel-triage`](https://github.com/IronSecCo/ironclaw/tree/main/examples/channel-triage)
template — read it alongside this page.

## Prerequisites

- A running IronClaw control-plane. The fastest way to one is the
  [Quickstart](../quickstart.md#your-first-approved-action) (`--dev` mode, loopback) or a real
  [deployment](https://github.com/IronSecCo/ironclaw/blob/main/README.md#deployment).
- The `ironctl` binary on your `PATH` (built with `CGO_ENABLED=1 go build -o bin/ ./cmd/ironctl`).
- [`jq`](https://jqlang.github.io/jq/) — used below to read the new messaging group's id.
- Admin access to a Slack workspace.

## 1. Create a Slack bot and get its token

1. Go to the [Slack API console](https://api.slack.com/apps) → **Create New App** → *From scratch*.
2. Open **OAuth & Permissions** → under **Scopes**, add the **`chat:write`** bot scope (so the bot
   can post messages).
3. **Install** the app to your workspace.
4. Copy the **Bot User OAuth Token** — it starts with `xoxb-…`. This is the only secret you need.
5. Invite the bot into the channel you want it in (e.g. `/invite @your-bot`), and note the channel
   **id** (`C…`) — in Slack, open the channel, click its name → the id is at the bottom of the
   *About* tab.

## 2. Give the token to the control-plane

The Slack adapter **auto-registers from an environment variable** when the daemon boots. Set
`SLACK_BOT_TOKEN` in the control-plane's environment **before** you start it:

```sh
export SLACK_BOT_TOKEN=xoxb-your-bot-token   # held host-side; never enters a sandbox
./bin/controlplane --dev --api-addr 127.0.0.1:8787
```

On boot the daemon logs:

```
channel adapter registered  adapter=slack
```

That line is your confirmation the adapter is live.

!!! info "Where the secret lives"
    The token is held **host-side** by the control-plane and **never enters a sandbox**. The
    adapter also redacts its own credential from any error string, so a token can't leak into logs.
    See [Channel adapters](../channels.md) for the full credential reference.

If you run with Docker Compose instead, put `SLACK_BOT_TOKEN=xoxb-…` in your `.env` file and start
the stack as usual.

## 3. Point `ironctl` at the control-plane

In a second terminal:

```sh
export IRONCLAW_API_TOKEN=<your control-plane API token>
# --addr defaults to http://127.0.0.1:8787, so no extra flag is needed in dev
```

## 4. Wire the agent to the channel

These are the five registry calls that connect an agent group to the Slack channel. Replace
`C0123ABCD` with your channel id and `slack:U0FRONTDESK` with a real Slack user id (the bare handle
prefixed with `slack:`).

```sh
# 1) the agent group
ironctl registry agent-group put --id triage --name "Triage Bot" --folder triage

# 2) a Slack channel messaging group, strict about unknown senders
MG="$(ironctl registry messaging-group create \
  --channel slack --platform C0123ABCD --group --policy strict | jq -r .ID)"
echo "messaging-group id: $MG"

# 3) register a known sender and make them a member of the agent group
ironctl registry user   put --id slack:U0FRONTDESK --kind person --name "On-call"
ironctl registry member add --user slack:U0FRONTDESK --agent triage

# 4) the wiring: engage on @mention, known senders only, keep context, one shared session
ironctl registry wiring create --mg "$MG" --agent triage \
  --engage mention --scope known --ignored accumulate --session shared --priority 5

# 5) allow the agent to post back into the channel
ironctl registry destination add --agent triage --channel slack --platform C0123ABCD
```

What each piece does:

| Call | Purpose |
| --- | --- |
| `agent-group put` | Creates the `triage` agent group. |
| `messaging-group create --channel slack` | Binds a Slack channel as an inbound surface. `--group` marks it a channel (not a DM); `--policy strict` means messages from unregistered senders are not acted on. |
| `user put` + `member add` | Registers a known sender and grants them access to the agent. |
| `wiring create` | The engagement rules — `--engage mention` (only on `@mention`), `--scope known` (registered users only), `--ignored accumulate` (keep non-engaging messages as context), `--session shared` (one shared thread of context). |
| `destination add` | Lets the agent **deliver** replies back into the channel. |

## 5. Verify the wiring

```sh
ironctl registry messaging-group wirings --id "$MG"          # the wiring you just created
ironctl registry access --user slack:U0FRONTDESK --agent triage   # confirms the sender has access
```

Then, in Slack, `@mention` the bot from the on-call user's account. The agent engages, runs in its
sandbox, and posts the reply back into the channel.

## What to notice

- Because the policy is `strict` and the scope is `known`, a **stranger in the channel cannot make
  the bot act** — a safe default for a shared workspace.
- `--ignored accumulate` means the bot isn't blind to the conversation it stayed out of: when
  finally mentioned, it has the recent context.
- The bot only **posts where you allowed it** — delivery is gated by the `destination` you added,
  not implied by the inbound wiring.

## Other channels

The same five-step shape works for every **auto-registered** adapter — just change the env var and
`--channel` value:

| Channel | Env var | `--channel` |
| --- | --- | --- |
| Slack | `SLACK_BOT_TOKEN` | `slack` |
| Discord | `DISCORD_BOT_TOKEN` | `discord` |
| Telegram | `TELEGRAM_BOT_TOKEN` | `telegram` |

Teams, Signal, and iMessage take richer config and register explicitly — see
[Channel adapters](../channels.md) for every adapter's credential.

## Next steps

- **No adapter for your platform?** [Write a custom channel adapter](custom-channel-adapter.md).
- **See the credential reference** for every channel: [Channel adapters](../channels.md).
- **Browse runnable templates:**
  [`examples/`](https://github.com/IronSecCo/ironclaw/tree/main/examples).
