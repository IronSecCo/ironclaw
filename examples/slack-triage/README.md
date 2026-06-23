# Slack triage bot

A bot that sits in a Slack channel and **classifies/labels every incoming
message** — e.g. `bug` / `question` / `feature` / `urgent` — so a busy channel
sorts itself. Unlike [`channel-triage/`](../channel-triage/) (which stays quiet
and engages only on `@mention`), this one engages on *every* message to label it.

The classification is the model's job, so it runs **credential-free on the `mock`
provider** for the demo and switches to a real model in production with no wiring
change.

## Run it credential-free (mock provider, no keys)

The demo control-plane seeds an offline `mock-agent` (provider `mock`, no key, no
network). From the repo root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # one-time: build + start
./examples/slack-triage/run-mock.sh                       # feed sample messages
```

`run-mock.sh` sends a handful of sample channel messages and prints what comes
back. With `mock` the reply is a deterministic echo (it proves each message
reaches the agent and a reply is delivered); with a real model the *same* setup
returns an actual label per message.

Tear down with `docker compose -f docker-compose.demo.yml down`.

## Wire it for production

[`setup.sh`](setup.sh) configures the Slack-channel shape: an agent group, a Slack
messaging group that engages on **every** message (`--engage pattern --pattern .`)
from **anyone** (`--scope all`), and a destination so it can post the label back.

```sh
export IRONCLAW_API_TOKEN=…
cd examples/slack-triage && ./setup.sh   # edit the Slack channel id at the top first
```

To make it actually classify, give it a persona through the gateway approval flow,
e.g. *"For each message, reply with exactly one label: bug, question, feature, or
urgent."*

## What to notice

- **Engages on everything.** `--engage pattern --pattern .` matches any message, so
  every post gets triaged — contrast with `channel-triage/`'s `--engage mention`.
- **`--scope all`** triages messages from unregistered senders too (a public
  channel); switch to `--scope known` to restrict it.
- **Credential-free by construction.** The demo proves the path on `mock`; a real
  Slack token (`SLACK_BOT_TOKEN`) and a model key turn it live with no code change.
