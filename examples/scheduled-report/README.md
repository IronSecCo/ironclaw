# Scheduled report agent

An agent that **wakes itself on a schedule**, summarizes something, and posts the
result to a channel. The classic "every weekday at 09:00, post a status digest"
bot — built without any external cron: the agent schedules its *own* recurring
wake with the sanctioned [`schedule_task`](../../docs/skills.md) tool, and each
wake runs the summary and delivers it to a configured destination.

> `schedule_task` only re-queues a prompt for the agent's future self — it runs no
> code. Any privileged action that future prompt then needs still passes through
> the human-approval gateway. See [`docs/architecture.md`](../../docs/architecture.md).

## Run it credential-free (mock provider, no keys)

The zero-credential demo control-plane seeds an offline `mock-agent` whose `mock`
provider makes **no network call and needs no model key**, so the whole pipeline
(inbound → sandbox loop → provider → reply) runs on a stock laptop. From the repo
root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # one-time: build + start
./examples/scheduled-report/run-mock.sh                   # drive the recipe
```

`run-mock.sh` asks the agent to schedule a recurring daily summary (you'll see the
`schedule_task` tool fire and the host accept it), then asks it to summarize a
sample input. With the `mock` provider the summary is an echo that proves the
round-trip; swap in a real model (set `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` on the
control-plane) and the *same* wiring produces a real digest.

Tear down with `docker compose -f docker-compose.demo.yml down`.

## Wire it for production

[`setup.sh`](setup.sh) configures the production shape against a running
control-plane — an agent group and a delivery destination (e.g. a Slack channel)
it may post into:

```sh
export IRONCLAW_API_TOKEN=…          # your control-plane API token
cd examples/scheduled-report && ./setup.sh
```

Then ask the agent (through the gateway-approved chat/persona flow) to establish
its recurring wake, e.g. *"Every weekday at 09:00, summarize yesterday's deploys
and post it to #ops."* It will call `schedule_task` with `recurrence: daily`; on
each wake it summarizes and delivers to the destination `setup.sh` registered.

## What to notice

- **No external scheduler.** The recurrence lives inside the agent via
  `schedule_task` — durable across restarts (the host persists it) and visible
  with the `list_scheduled_tasks` tool.
- **The model is swappable.** `mock` proves the plumbing credential-free; a real
  provider does the actual summarizing with zero wiring changes.
- **Delivery is gated by a registered destination.** An agent can only post to a
  channel it has an explicit `destination` for — see `setup.sh`.
