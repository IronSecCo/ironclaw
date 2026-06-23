# Webhook responder

An agent that **receives an inbound HTTP message and replies**. Use it to turn any
system that can POST JSON (a form backend, an alerting tool, a CI hook) into a
conversation with an agent — the request comes in, the agent processes it, and the
reply flows back out through the normal delivery path.

IronClaw's chat ingress (`POST /v1/ui/chat/send`) **is** that inbound webhook: it
feeds the message through the real router (engage → session → encrypted inbound
queue → sandbox → reply), not a side door. The agent's outbound reply can be
polled back (as below) or delivered to an external URL via a `webhook`
[destination](../../docs/channels.md).

## Run it credential-free (mock provider, no keys)

The demo control-plane seeds an offline `mock-agent` (provider `mock`, no model
key, no network). From the repo root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # one-time: build + start
./examples/webhook-responder/run-mock.sh                  # POST a webhook, read the reply
```

`run-mock.sh` POSTs a sample webhook payload to the ingress and prints the agent's
reply. The `mock` provider echoes the request to prove the inbound→reply loop;
point the control-plane at a real model and the same endpoint becomes a genuine
"reply to my webhook" agent.

Tear down with `docker compose -f docker-compose.demo.yml down`.

## The inbound contract

```sh
curl -fsS -X POST http://127.0.0.1:8787/v1/ui/chat/send \
  -H "Authorization: Bearer $IRONCLAW_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"agentGroupID":"responder","text":"deploy #1421 failed on prod"}'
# → 202 { "conversationId": "...", "engaged": true, ... }

curl -fsS http://127.0.0.1:8787/v1/ui/chat/responder/messages \
  -H "Authorization: Bearer $IRONCLAW_API_TOKEN"
# → { "messages": [ { "text": "..." } ] }      # drained once
```

## Wire it for production

[`setup.sh`](setup.sh) creates the agent group and (optionally) a `webhook`
**destination** so replies are POSTed to *your* URL instead of being polled:

```sh
export IRONCLAW_API_TOKEN=…
cd examples/webhook-responder && ./setup.sh
```

## What to notice

- **Real pipeline, not a shortcut.** Inbound webhooks traverse the same encrypted
  queue, engage policy, and approval gateway as every other channel.
- **Two reply modes.** Poll `/messages` (pull) or register a `webhook` destination
  to have replies POSTed to you (push).
- **The model is swappable.** `mock` proves the loop credential-free; a real
  provider answers for real with no wiring change.
