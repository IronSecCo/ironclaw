---
title: Examples & use cases
description: A gallery of runnable IronClaw agent recipes — pick a card, copy the command, watch it run. Five need no credentials at all.
---

# Examples & use cases

Every recipe below is a self-contained directory in
[`examples/`](https://github.com/IronSecCo/ironclaw/tree/main/examples) — a `README.md`
that explains it and a script with the exact `ironctl` commands. Pick a card, copy the
command, and watch it run. **Five of the nine need no credentials at all** — no model
key, no channel token.

Not sure where to start? Run [**`hello-ironclaw`**](#run-it-now-no-credentials) — it
proves the whole secured path end-to-end in one command.

## Run it now, no credentials { #run-it-now-no-credentials }

Self-contained: one command builds the sandbox image, brings up the offline demo
control-plane, runs the scenario, asserts the result, and tears everything down.
Requires only Docker.

<div class="grid cards" markdown>

-   :material-hand-wave: **[hello-ironclaw](https://github.com/IronSecCo/ironclaw/tree/main/examples/hello-ironclaw)** &nbsp;·&nbsp; *zero-cred · self-contained*

    ---

    ![hello-ironclaw terminal demo: one command builds the sandbox image, starts the offline mock-agent, sends a chat through the real engage to per-session sandbox to encrypted queue to reply path, and prints PASS when the reply returns.](assets/hello.svg){ loading=lazy }

    The canonical first "it works": sends a chat through the **real** secured path
    (engage → per-session sandbox → encrypted queue → delivery) and **asserts** the
    reply comes back. Doubles as the CI smoke test.

    ```sh
    examples/hello-ironclaw/run.sh
    ```

-   :material-shield-sword: **[red-team-escape](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape)** &nbsp;·&nbsp; *zero-cred · self-contained*

    ---

    ![red-team-escape terminal demo: assuming a fully jailbroken agent, an escape battery runs from inside the sandbox and prints a PASS table (network egress blocked, Docker socket absent, no sibling orchestration, host root not mounted, self-modification held at the gateway, master and sibling keys unreachable), then reports every core containment assertion held.](assets/redteam.svg){ loading=lazy }

    The other side of the coin: assumes a fully jailbroken agent and **tries to break
    out** — network egress, host escape via the Docker socket, sibling breakout,
    self-modification — then asserts every attack is contained.

    ```sh
    examples/red-team-escape/run.sh
    ```

</div>

## The whole pipeline, still credential-free { #credential-free-mock }

These drive the **entire** inbound → agent → reply pipeline against the offline `mock`
provider. Bring the demo control-plane up once (it seeds an offline `mock-agent` that
makes no network call and needs no key), then run any recipe from the repo root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # seeds the offline mock-agent
# … run one or more recipes below …
docker compose -f docker-compose.demo.yml down            # tear down
```

<div class="grid cards" markdown>

-   :material-clock-outline: **[scheduled-report](https://github.com/IronSecCo/ironclaw/tree/main/examples/scheduled-report)** &nbsp;·&nbsp; *credential-free (mock)*

    ---

    An agent that wakes **itself** on a schedule via the `schedule_task` tool,
    summarizes, and posts the digest to a channel — no external cron.

    ```sh
    ./examples/scheduled-report/run-mock.sh
    ```

-   :material-webhook: **[webhook-responder](https://github.com/IronSecCo/ironclaw/tree/main/examples/webhook-responder)** &nbsp;·&nbsp; *credential-free (mock)*

    ---

    An inbound HTTP webhook routed through the real pipeline to an agent that replies —
    poll the reply or push it back via a `webhook` destination.

    ```sh
    ./examples/webhook-responder/run-mock.sh
    ```

-   :material-label-multiple: **[slack-triage](https://github.com/IronSecCo/ironclaw/tree/main/examples/slack-triage)** &nbsp;·&nbsp; *credential-free (mock)*

    ---

    A bot that classifies and labels **every** incoming Slack message
    (`bug` / `question` / `feature` / `urgent`).

    ```sh
    ./examples/slack-triage/run-mock.sh
    ```

</div>

!!! note "With `mock`, the replies are deterministic echoes that prove the pipeline."
    Set a real model credential on the control-plane (`ANTHROPIC_API_KEY`,
    `OPENAI_API_KEY`, …) and the **same** recipes do real work with no wiring change.

## Bring your own channel { #bring-your-own-channel }

These configure a real channel wiring, so they need a
[running control-plane](quickstart.md) and your own channel token. Each `setup.sh` is
idempotent where the API allows; identifiers in the scripts (channel ids, handles) are
placeholders — edit them for your setup.

<div class="grid cards" markdown>

-   :material-account: **[personal-assistant](https://github.com/IronSecCo/ironclaw/tree/main/examples/personal-assistant)** &nbsp;·&nbsp; *your channel*

    ---

    A private 1:1 assistant on Telegram that replies to every message — plus a
    walk-through of the mandatory change-approval flow.

    ```sh
    cd examples/personal-assistant && ./setup.sh
    ```

-   :material-message-text: **[channel-triage](https://github.com/IronSecCo/ironclaw/tree/main/examples/channel-triage)** &nbsp;·&nbsp; *your channel*

    ---

    A quiet triage bot in a shared Slack channel: engages only on `@mention`, only for
    known senders, and accumulates the context it stayed out of.

    ```sh
    cd examples/channel-triage && ./setup.sh
    ```

-   :material-account-group: **[multi-agent-team](https://github.com/IronSecCo/ironclaw/tree/main/examples/multi-agent-team)** &nbsp;·&nbsp; *your channel*

    ---

    Two agents sharing one group chat (a frontline responder + a scribe), showing
    priorities, multi-agent wiring, and where `create_agent` sits.

    ```sh
    cd examples/multi-agent-team && ./setup.sh
    ```

-   :material-eye-outline: **[keyword-watcher](https://github.com/IronSecCo/ironclaw/tree/main/examples/keyword-watcher)** &nbsp;·&nbsp; *your channel*

    ---

    A quiet ops agent in a Discord channel that engages only on a `pattern` match
    (`deploy` / `incident` / `outage`), one session per incident thread.

    ```sh
    cd examples/keyword-watcher && ./setup.sh
    ```

</div>

## See also

- [Quickstart](quickstart.md) — the zero-credential demo these recipes build on.
- [Tutorials](tutorials/index.md) — guided walkthroughs from clone to a running agent.
- [Channel adapters](channels.md) — the 12 channels a recipe can ingest from and post to.
