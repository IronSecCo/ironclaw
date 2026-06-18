# Multi-agent team (one channel, two agents)

Two agent groups wired into the **same** Slack channel, showing how engage mode
and priority let multiple agents share a space without stepping on each other:

- **`frontline`** — responds when `@mentioned` (priority 10).
- **`scribe`** — watches for the word `summary` and produces recaps (priority 1).

## What it configures

- Two agent groups: `frontline` and `scribe`.
- One shared Slack channel messaging group.
- Two wirings into that group:
  - `frontline`: `--engage mention --priority 10`.
  - `scribe`: `--engage pattern --pattern 'summary' --priority 1`.
- A delivery destination for each agent.

## Try it

```sh
cd examples/multi-agent-team
./setup.sh
```

Edit the Slack channel id at the top of [`setup.sh`](setup.sh).

## What to notice

- **Priority** breaks ties when more than one wiring could engage; the higher
  number wins, so `frontline` takes a direct mention while `scribe` handles the
  `summary` pattern.
- **Agent-to-agent (a2a) messaging** between the two groups is mediated by the
  control-plane (host-routed and audited) — agents never talk peer-to-peer.
- **Spawning a new agent at runtime** (`create_agent`) is privileged: it always
  routes through the gateway's **mandatory human-approval** floor and is never
  auto-approved — a new agent is a new trust principal. See
  [`docs/threat-model.md`](../../docs/threat-model.md) (boundary B3).
