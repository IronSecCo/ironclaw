---
title: Examples
description: Copy-pasteable, runnable agent recipes — three run end-to-end with no credentials.
---

# Examples

Runnable recipes live in [`examples/`](https://github.com/IronSecCo/ironclaw/tree/main/examples)
— each is a self-contained directory with a `README.md` (what it does) and a
`setup.sh` (the exact `ironctl` commands). Three of them also ship a `run-mock.sh`
that drives the **whole** inbound → agent → reply pipeline against the offline
`mock` provider, so a fresh clone runs them with **no model key and no channel
tokens**.

## Run a recipe end-to-end, credential-free

Bring up the [zero-credential demo control-plane](quickstart.md) once — it seeds an
offline `mock-agent` whose `mock` provider makes no network call and needs no key —
then run any recipe from the repo root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # seeds the offline mock-agent
./examples/scheduled-report/run-mock.sh                   # cron-style self-scheduling summary
./examples/webhook-responder/run-mock.sh                  # inbound webhook → agent reply
./examples/slack-triage/run-mock.sh                       # classify/label every message
docker compose -f docker-compose.demo.yml down            # tear down
```

With `mock` the replies are deterministic echoes that prove the pipeline; set a
real model credential on the control-plane (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
…) and the *same* recipes do real work with no wiring change.

## The recipes

<div class="grid cards" markdown>

-   :material-clock-outline: **[Scheduled report](https://github.com/IronSecCo/ironclaw/tree/main/examples/scheduled-report)** · *credential-free demo*

    An agent that wakes itself on a schedule via the `schedule_task` tool,
    summarizes, and posts the digest to a channel — no external cron.

-   :material-webhook: **[Webhook responder](https://github.com/IronSecCo/ironclaw/tree/main/examples/webhook-responder)** · *credential-free demo*

    An inbound HTTP webhook routed through the real pipeline to an agent that
    replies — poll the reply or push it back via a `webhook` destination.

-   :material-label-multiple: **[Slack triage](https://github.com/IronSecCo/ironclaw/tree/main/examples/slack-triage)** · *credential-free demo*

    A bot that classifies/labels **every** incoming Slack message
    (`bug`/`question`/`feature`/`urgent`).

-   :material-account: **[Personal assistant](https://github.com/IronSecCo/ironclaw/tree/main/examples/personal-assistant)**

    A private 1:1 assistant on Telegram that replies to every message — plus a
    walk-through of the mandatory change-approval flow.

-   :material-message-text: **[Channel triage](https://github.com/IronSecCo/ironclaw/tree/main/examples/channel-triage)**

    A quiet triage bot in a shared Slack channel: engages only on `@mention`, only
    for known senders, and accumulates the context it stayed out of.

-   :material-account-group: **[Multi-agent team](https://github.com/IronSecCo/ironclaw/tree/main/examples/multi-agent-team)**

    Two agents sharing one group chat (a frontline responder + a scribe), showing
    priorities, multi-agent wiring, and where `create_agent` sits.

-   :material-eye-outline: **[Keyword watcher](https://github.com/IronSecCo/ironclaw/tree/main/examples/keyword-watcher)**

    A quiet ops agent in a Discord channel that engages only on a `pattern` match
    (`deploy`/`incident`/`outage`), one session per incident thread.

</div>

## See also

- [Quickstart](quickstart.md) — the zero-credential demo these recipes build on.
- [Tutorials](tutorials/index.md) — guided walkthroughs from clone to a running agent.
- [Channel adapters](channels.md) — the channels a recipe can ingest from and post to.
