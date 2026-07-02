# ci-action — run a sandboxed IronClaw agent in GitHub Actions

The reusable [`IronClaw` action](../../.github/actions/ironclaw) lets a team run a
one-shot IronClaw agent task in CI without any local setup: point a workflow step at
it with a `prompt`, and it brings up the offline control-plane, sends the prompt
through the **real** secured route (engage → per-session Docker sandbox → encrypted
queue → reply), and hands back the reply plus a JSON run report. With
`containment-report: true` it also freezes the signed-able isolation proof for the
exact build under test.

Credential-free by default: the `mock` provider makes no network call, so the whole
thing runs on a stock `ubuntu-latest` runner with nothing but Docker — no model key,
no channel tokens.

## Copy-paste usage

```yaml
# .github/workflows/ironclaw.yml
name: IronClaw agent
on: [push]
permissions:
  contents: read
jobs:
  agent:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v7
      - id: agent
        uses: IronSecCo/ironclaw/.github/actions/ironclaw@main   # pin to a release tag in production
        with:
          prompt: "summarize the change in this push"
          provider: mock            # credential-free; a real provider needs a self-hosted control-plane
      - run: echo "${AGENT_REPLY}"
        env:
          AGENT_REPLY: ${{ steps.agent.outputs.reply }}
      - uses: actions/upload-artifact@v7
        with:
          name: ironclaw-run-report
          path: ${{ steps.agent.outputs.report-path }}
```

## Inputs / outputs

| Input | Default | Meaning |
|-------|---------|---------|
| `prompt` | _(required)_ | the prompt sent to the agent |
| `provider` | `mock` | model provider; only `mock` is credential-free |
| `model` | _(empty)_ | model id (recorded in the run report) |
| `agent-group` | `mock-agent` | the agent group id to engage |
| `config` | _(empty)_ | optional agent config path (recorded; the mock path ignores it) |
| `containment-report` | `false` | also emit the containment proof for the build under test |
| `report-dir` | _(runner temp)_ | where artifacts are written |

| Output | Meaning |
|--------|---------|
| `reply` | the agent reply text |
| `report-path` | directory holding the run report (and containment report, if requested) |
| `run-report` | path to `ironclaw-run.json` (prompt, reply, verdict, commit) |
| `containment-report` | path to `containment-report.json` (only when `containment-report: true`) |

## Proven in this repo

[`.github/workflows/ironclaw-action-example.yml`](../../.github/workflows/ironclaw-action-example.yml)
runs this action credential-free on every relevant push/PR — one job proves the
agent round-trip, one job emits and uploads the containment report — so a regression
in the action fails a build here before it reaches an adopter. Full docs:
[Integrations → CI (GitHub Action)](../../docs/integrations/ci.md).

## Real providers

`mock` is deterministic (the reply echoes the prompt) and proves the pipeline with no
secrets. To run a real model you host your own IronClaw control-plane with the model
credential set on it and a matching agent group registered, then point the workflow
at it (set `IRONCLAW_ADDR` / `IRONCLAW_API_TOKEN` in the step env). The action never
takes a raw model key as an input — credentials stay host-side, custodied by the
control-plane. See [docs/integrations/ci.md](../../docs/integrations/ci.md).
