---
title: Run IronClaw in CI (GitHub Action)
description: A reusable GitHub Action that runs a sandboxed IronClaw agent against a prompt in CI, credential-free, and captures the reply plus a containment report.
---

# Run IronClaw in CI (GitHub Action)

CI is the most common place teams automate work, and it is the fastest way to try
IronClaw without any local setup. The reusable **`IronClaw` action** runs a one-shot
sandboxed agent task from a workflow step: give it a `prompt`, and it brings up the
offline control-plane, sends the prompt through the **real** secured route
(engage → per-session Docker sandbox → encrypted queue → reply), and returns the
reply plus a JSON run report. Ask for the containment report and it also freezes the
signed-able isolation proof for the exact build under test.

It is a thin wrapper over the same zero-credential path
[`hello-ironclaw`](../../examples/hello-ironclaw/) and
[`red-team-escape`](../../examples/red-team-escape/) use — no core control-plane
changes, nothing you cannot also run by hand.

## Credential-free by default

The `mock` provider makes no network call, so the action runs on a stock
`ubuntu-latest` runner with nothing but Docker — no model key, no channel tokens.
The reply is a deterministic echo of the prompt, which is enough to prove the whole
pipeline works.

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
          provider: mock
      - run: echo "${AGENT_REPLY}"
        env:
          AGENT_REPLY: ${{ steps.agent.outputs.reply }}
      - uses: actions/upload-artifact@v7
        with:
          name: ironclaw-run-report
          path: ${{ steps.agent.outputs.report-path }}
```

Pass untrusted values (an agent reply, a PR title) through `env:` and reference them
as quoted shell variables, never inline in a `run:` string — the snippet above does
this for the reply.

## Emit the containment report

Set `containment-report: true` to also run the red-team-escape harness and freeze the
isolation proof for the build under test, then upload it as a downloadable artifact:

```yaml
      - id: agent
        uses: IronSecCo/ironclaw/.github/actions/ironclaw@main
        with:
          prompt: "prove the sandbox holds"
          provider: mock
          containment-report: "true"
      - uses: actions/upload-artifact@v7
        with:
          name: ironclaw-containment-report
          path: ${{ steps.agent.outputs.report-path }}
```

The report is the same machine-verifiable JSON (`schemaVersion 1.0`) that
[release.yml](release-runbook.md) signs and attaches to every GitHub Release — the
action produces a fresh one from *this* run's real result rows, never a fabricated
verdict.

## Inputs

| Input | Default | Meaning |
|-------|---------|---------|
| `prompt` | _(required)_ | the prompt sent to the agent |
| `provider` | `mock` | model provider; only `mock` is credential-free |
| `model` | _(empty)_ | model id (recorded in the run report) |
| `agent-group` | `mock-agent` | the agent group id to engage |
| `config` | _(empty)_ | optional agent config path (recorded; the mock path ignores it) |
| `containment-report` | `false` | also emit the containment proof for the build under test |
| `report-dir` | _(runner temp)_ | where artifacts are written |

## Outputs

| Output | Meaning |
|--------|---------|
| `reply` | the agent reply text |
| `report-path` | directory holding the run report (and containment report, if requested) |
| `run-report` | path to `ironclaw-run.json` (prompt, reply, verdict, commit) |
| `containment-report` | path to `containment-report.json` (only when `containment-report: true`) |

## Real providers

`mock` proves the pipeline with no secrets. To run a real model, host your own
IronClaw control-plane with the model credential set on it (credentials stay
host-side, custodied by the control-plane — the action never takes a raw model key as
an input) and a matching agent group registered, then point the workflow at it by
setting `IRONCLAW_ADDR` and `IRONCLAW_API_TOKEN` in the step environment.

## Proven in this repo

[`.github/workflows/ironclaw-action-example.yml`](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/ironclaw-action-example.yml)
runs the action credential-free on every relevant push/PR — one job proves the agent
round-trip, one job emits and uploads the containment report — so a regression in the
action turns a build red before it reaches an adopter. The runnable example lives in
[`examples/ci-action/`](https://github.com/IronSecCo/ironclaw/tree/main/examples/ci-action).

!!! note "Not on the Marketplace yet"
    The action carries Marketplace metadata but is not published there yet
    (owner-manual, launch-gated). Reference it by repository path
    (`IronSecCo/ironclaw/.github/actions/ironclaw@<ref>`) in the meantime.
