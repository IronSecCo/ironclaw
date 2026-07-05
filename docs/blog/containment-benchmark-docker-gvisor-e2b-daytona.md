---
title: "We ran the same escape suite against Docker, gVisor, E2B, and Daytona"
description: "A reproducible containment benchmark for AI-agent sandboxes. One fixed escape-attempt suite, run from inside each posture, scored by observed behavior. Raw Docker blocked 2 of 5, hardened runc 4 of 5, gVisor 5 of 5. Honest labels for the hosted platforms, no fabricated numbers, re-runnable in CI."
---

# We ran the same escape suite against Docker, gVisor, E2B, and Daytona

Most "sandbox comparison" posts compare feature checklists. That tells you almost
nothing about the question that actually matters when an autonomous agent turns
hostile: **how many escape attempts does the boundary actually block?**

So we built one fixed escape-attempt suite and ran it from *inside* several sandbox
postures, then counted what got through. The whole thing is a committed script that
runs in CI, so the numbers cannot quietly drift from reality.

> A plain Docker container blocked 2 of 5 escape attempts. A hardened container blocked
> 4 of 5, but still shares the host kernel. gVisor blocked 5 of 5. That last attempt is
> the whole ballgame.

## Why we did this

We ship [IronClaw](https://github.com/IronSecCo/ironclaw), a security-first, self-hosted
runtime for AI agents. Our whole pitch is a containment boundary you can verify, so
"trust us, it's secure" is exactly the wrong thing to say. We wanted a number, produced
by a script anyone can re-run, not an adjective.

The threat model is the uncomfortable one: assume the agent is **fully compromised and
running arbitrary code inside its sandbox.** This is not about whether the model can be
tricked (prompt injection is a different layer). It is: *when* it is compromised, does
the isolation hold? Each probe is a real attack primitive run as the container's own
unprivileged process.

## The escape-attempt suite

Five attempts, each scored `OPEN` (the attack succeeded, bad) or `BLOCKED` (contained,
good), by observing real behavior:

| # | Attempt | What it proves |
| --- | --- | --- |
| 1 | `net.nic` | No network interface but loopback |
| 2 | `net.egress` | No outbound reachability |
| 3 | `fs.host` | Host root not in the mount namespace |
| 4 | `caps.privileged` | Privileged syscalls dropped (`mount(2)` needs `CAP_SYS_ADMIN`) |
| 5 | `kernel.shared-host` | Host kernel not reached directly |

Attempt 5 is the one that separates a **shared-kernel** runtime from an
**isolated-kernel** one. Under raw runc, every syscall the compromised agent makes is
served by the *host* kernel. Its full syscall and CVE surface is one bug away. Under
gVisor, a user-space kernel (the Sentry) serves those syscalls, so the host kernel is
never the thing the agent is talking to.

## The results

Measured on `ubuntu-24.04` by the
[containment-matrix harness](https://github.com/IronSecCo/ironclaw/blob/main/scripts/bench/containment-matrix.sh).
The harness asserts each target's expected posture and exits non-zero on divergence, so
it doubles as a regression gate:

| Target | Attempts | Blocked | Block rate | Measured |
| --- | ---: | ---: | ---: | :---: |
| raw Docker (default runc) | 5 | 2 | 40% | yes |
| hardened Docker (runc) | 5 | 4 | 80% | yes |
| gVisor / runsc (IronClaw runtime) | 5 | 5 | 100% | yes |
| E2B (hosted) | - | - | sourced | no |
| Daytona (hosted) | - | - | sourced | no |

Per-attempt breakdown for the measured rows:

| Attempt | raw Docker | hardened runc | gVisor |
| --- | :---: | :---: | :---: |
| `net.nic` | OPEN | BLOCKED | BLOCKED |
| `net.egress` | OPEN | BLOCKED | BLOCKED |
| `fs.host` | BLOCKED | BLOCKED | BLOCKED |
| `caps.privileged` | BLOCKED | BLOCKED | BLOCKED |
| `kernel.shared-host` | OPEN | OPEN | BLOCKED |

The interesting line is the last one. Hardening a container with `network=none`,
dropped capabilities, and a read-only rootfs gets you from 40% to 80%, and that is real,
worthwhile hardening. But it does nothing about `kernel.shared-host`: the agent is still
talking to your host kernel. gVisor is the row where that attempt finally flips to
`BLOCKED`, because a user-space kernel is now in the path.

## About E2B and Daytona (sourced, not measured)

Here is where we refuse to fabricate. E2B and Daytona are hosted platforms that need
vendor accounts, so we cannot run the suite against them in a secret-free CI job. Rather
than invent a block rate, we describe the architectural pattern from published docs and
label the row **sourced**. You should verify the current specifics against each
product's own documentation before relying on them.

- **E2B** runs sandboxes in **Firecracker microVMs**. A microVM has its own guest
  kernel, so the `kernel.shared-host` attempt would not apply the way it does to a
  shared-kernel container. That is a strong isolation story. The trade is the hosted
  one: a third party runs the VM, and in most agent setups the keys and data flow
  through it. Network egress is generally available to sandboxes by default; confirm the
  current posture in E2B's docs.
- **Daytona** provides **container-based** sandboxes for running AI-generated code. A
  container's containment depends on how it is configured: network policy, dropped
  capabilities, and whether a user-space kernel like gVisor is in the path. Treat its
  posture as configuration-dependent and verify it against Daytona's docs.

The honest summary: a **microVM** (E2B) and a **user-space-kernel container** (IronClaw
on gVisor) both close the shared-host-kernel gap that a plain container leaves open.
They differ mostly on the *managed vs self-hosted* axis, which is really the question of
who holds your keys and data. A **plain container platform** closes that gap only if you
configure it to.

## What a runtime does not cover

The gVisor row is IronClaw's *runtime*. A benchmark of runtimes is honest about its own
scope: a runtime has no opinion about the rest of the agent lifecycle. IronClaw stacks
two more containment assertions on top that a bare runtime does not provide, proven by
the [red-team-escape gate](https://github.com/IronSecCo/ironclaw/blob/main/examples/red-team-escape/run.sh)
on every push:

- **Self-modification is held, not applied.** An agent that asks to widen its own
  capabilities has the request parked at a human-approval gateway. It cannot grant
  itself the tool.
- **Key custody holds across sessions.** The host master key and sibling sessions' keys
  are never mounted into a sandbox; each box binds only its own per-session subtree.

Those are platform controls, not runtime controls, which is why they are a separate
column in the full [containment benchmark page](../compare/sandbox-containment-benchmark.md).

## Reproduce it yourself

The whole benchmark is one script. On a Linux host with Docker (and, for the gVisor row,
`runsc` registered as a Docker runtime):

```bash
# fast, no runtime needed: validates the scoring / regression logic
scripts/bench/containment-matrix.sh --self-test

# the measured run (writes results.md, results.json, methodology.txt)
scripts/bench/containment-matrix.sh --out ./matrix-results
```

To reproduce the exact published numbers, run the
[containment-matrix workflow](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/containment-matrix.yml)
on `ubuntu-24.04`. It installs `runsc`, registers it with Docker, and uploads the raw
`results.json` as an artifact.

If you want to poke at the boundary directly instead, the
[quickstart](../quickstart.md) goes from a clean clone to a held approval in about five
minutes, and [breaking our own sandbox](../breaking-our-own-sandbox.md) walks the same
escape attempts by hand.

## Read more

- The full comparison page with the complete methodology and threat model:
  [sandbox containment benchmark](../compare/sandbox-containment-benchmark.md).
- Why a shared-kernel container is not enough:
  [IronClaw vs raw Docker](../compare/ironclaw-vs-docker-agent-sandbox.md) and
  [IronClaw vs gVisor alone](../compare/ironclaw-vs-gvisor-alone.md).
- The performance side of the same trade:
  [sandbox benchmarks](../benchmarks.md).
- The threat model the suite is derived from: [threat model](../threat-model.md).
