---
title: "Sandbox containment benchmark: gVisor vs runc, E2B, Daytona"
description: "A reproducible head-to-head containment benchmark. One fixed escape-attempt suite run against raw Docker, hardened runc, and gVisor, plus how hosted sandboxes like E2B and Daytona compare. Honest labels, no fabricated numbers, re-runnable in CI."
---

# Sandbox containment benchmark

Most "sandbox comparison" pages compare *features*. This one compares the only thing
that matters when an agent turns hostile: **how many escape attempts the boundary
actually blocks.** We take one fixed escape-attempt suite - the same threat model as
IronClaw's [red-team containment gate](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape) -
and run it from *inside* several sandbox postures, then count what got through.

The measured rows are produced by a committed, re-runnable harness
([`scripts/bench/containment-matrix.sh`](https://github.com/IronSecCo/ironclaw/blob/main/scripts/bench/containment-matrix.sh))
that runs in CI on `ubuntu-24.04`. Where a target cannot be run in a secret-free CI job
(the hosted platforms need vendor accounts), we describe its architecture from published
docs and label the row **sourced** - we never invent a number.

## Threat model

We assume the worst: the agent is **fully compromised and can run arbitrary code**
inside its sandbox. This is not "can the model be tricked" (prompt injection is a
different layer) - it is "*when* it is, does the isolation boundary hold." Each probe is
a real attack primitive run as the container's own unprivileged process.

## The escape-attempt suite

Five attempts, each scored `OPEN` (the attack succeeded - bad) or `BLOCKED` (contained -
good), by observing real behavior:

| # | Attempt | What it proves | How it is judged |
| --- | --- | --- | --- |
| 1 | `net.nic` | No network interface but loopback | enumerate `/sys/class/net` |
| 2 | `net.egress` | No outbound reachability | connect to a public IP the box has no business reaching |
| 3 | `fs.host` | Host root not in the mount namespace | probe host-only paths |
| 4 | `caps.privileged` | Privileged syscalls dropped | attempt `mount(2)` (needs `CAP_SYS_ADMIN`) |
| 5 | `kernel.shared-host` | Host kernel not reached directly | is `/proc/version` the host kernel, or an intercepting user-space kernel? |

Attempt 5 is the one that separates a **shared-kernel** runtime from an
**isolated-kernel** one. Under raw runc every syscall is served by the *host* kernel -
its full syscall and CVE surface is one bug away. Under gVisor, a user-space kernel
(Sentry) serves the syscalls, so the host kernel is never the one the compromised agent
is talking to.

## Results

Measured on `ubuntu-24.04` by the containment-matrix harness. The exact numbers are
regenerated on every run of the
[containment-matrix workflow](https://github.com/IronSecCo/ironclaw/actions/workflows/containment-matrix.yml)
(download the `containment-matrix-results` artifact for the raw `results.json`). The
harness also asserts each target's expected posture and turns **red** on divergence, so
this table cannot silently drift from reality.

| Target | Escape attempts | Blocked | Block rate | Measured | Notes |
| --- | ---: | ---: | ---: | :---: | --- |
| raw Docker (default runc) | 5 | 2 | 40% | yes | unhardened container: bridge NIC, default caps, shared host kernel |
| hardened Docker (runc) | 5 | 4 | 80% | yes | `network=none` + caps dropped + read-only; **still shares the host kernel** |
| gVisor / runsc (IronClaw runtime) | 5 | 5 | 100% | yes | IronClaw's production posture: user-space kernel, host kernel never reached |
| **IronClaw (full platform)** | 5 + 2 | 7 | 100% | yes | gVisor runtime **plus** two platform assertions no bare runtime has (below) |
| E2B (hosted) | - | - | sourced | no | Firecracker microVM: separate guest kernel; egress on by default. Verify against E2B docs |
| Daytona (hosted) | - | - | sourced | no | container-based sandboxes; posture depends on configuration. Verify against Daytona docs |

Per-attempt breakdown for the measured rows:

| Attempt | raw Docker | hardened runc | gVisor |
| --- | :---: | :---: | :---: |
| `net.nic` | OPEN | BLOCKED | BLOCKED |
| `net.egress` | OPEN | BLOCKED | BLOCKED |
| `fs.host` | BLOCKED | BLOCKED | BLOCKED |
| `caps.privileged` | BLOCKED | BLOCKED | BLOCKED |
| `kernel.shared-host` | OPEN | OPEN | BLOCKED |

### What "IronClaw (full platform)" adds

The gVisor row is IronClaw's *runtime*. The platform stacks two more containment
assertions on top that no bare runtime provides, proven by the
[red-team-escape gate](https://github.com/IronSecCo/ironclaw/blob/main/examples/red-team-escape/run.sh)
on every push:

- **Self-modification is held, not applied.** An agent that asks to widen its own
  capabilities has the request parked at a human-approval gateway - it cannot grant
  itself the tool. (A runtime has no opinion about this; it is a platform control.)
- **Key custody holds across sessions.** The host master key and sibling sessions' keys
  are never mounted into a sandbox; each box binds only its own per-session subtree.

Those are why "runtime containment" and "platform containment" are different columns.

## How the hosted platforms compare (sourced, not measured)

We describe the architectural pattern and do not assert the current specifics of any
named product - those change, and you should verify them against that product's own
docs before relying on them.

- **E2B** runs sandboxes in **Firecracker microVMs**. A microVM has its own guest
  kernel, so the `kernel.shared-host` attempt would not apply the way it does to a
  shared-kernel container - a strong isolation story. The trade is the hosted one: a
  third party runs the VM and, in most agent setups, the keys and data flow through it
  (see [IronClaw vs hosted sandboxes](ironclaw-vs-e2b-hosted-sandboxes.md)). Network
  egress is generally available to sandboxes by default; confirm the current posture in
  E2B's documentation.
- **Daytona** provides **container-based** sandboxes for running AI-generated code. A
  container's containment depends on how it is configured (network policy, dropped
  capabilities, and whether a user-space kernel like gVisor is in the path). Treat its
  posture as configuration-dependent and verify it against Daytona's documentation.

The honest summary: a **microVM** (E2B) and a **user-space-kernel container** (IronClaw
on gVisor) both close the shared-host-kernel gap that a plain container leaves open;
they differ mostly on the *managed vs self-hosted* axis - who holds your keys and data.
A **plain container platform** closes it only if you configure it to.

## Reproduce it yourself

The whole benchmark is one script. On a Linux host with Docker (and, for the gVisor
row, `runsc` registered as a Docker runtime):

```bash
# fast, no runtime needed: validates the scoring / regression logic
scripts/bench/containment-matrix.sh --self-test

# the measured run (writes results.md, results.json, methodology.txt)
scripts/bench/containment-matrix.sh --out ./matrix-results
```

To reproduce the exact published numbers, run the
[containment-matrix workflow](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/containment-matrix.yml)
on `ubuntu-24.04`; it installs `runsc`, registers it with Docker, and uploads the
results as an artifact.

## Where to go next

- The performance side of the same trade: [sandbox benchmarks](../benchmarks.md) -
  gVisor overhead vs a host baseline.
- Why a shared-kernel container is not enough: [IronClaw vs gVisor alone](ironclaw-vs-gvisor-alone.md)
  and [IronClaw vs raw Docker](ironclaw-vs-docker-agent-sandbox.md).
- The full threat model the suite is derived from: [threat model](../threat-model.md).
- Run the self-hosted path in minutes: [quickstart](../quickstart.md).
