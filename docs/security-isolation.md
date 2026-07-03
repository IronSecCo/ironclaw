---
title: "Security and isolation: proof, not promises"
description: One page that makes IronClaw's security differentiator legible. See the isolation architecture, the red-team attempts we run against our own sandbox, and the measured overhead you pay for the wall.
---

# Security and isolation

Most AI-agent tools ask you to trust the agent. IronClaw does not. It assumes the
agent inside the sandbox is already compromised, running attacker code as its own
user, and is built so that even then it cannot escape the box, read another
session, reach the network, or change its own permissions without a human.

This page is the proof of that claim in one place: the architecture that draws the
line, the attacks we run against our own sandbox to test the line, and the
measured cost of holding it.

## How the box is drawn

The host is the trust root. The agent is not. Everything the agent can touch lives
on the left of the wall below. The only ways across are a handful of host-owned
choke points, and every change the agent asks for is held for a human.

<figure markdown="span">
  [![IronClaw isolation architecture: an untrusted gVisor sandbox on the left, separated by the B1 gVisor wall from the trusted host on the right. The only crossings are host-owned unix sockets and per-session encrypted queues. A legend maps each of six red-team escape attempts to the control that contains it.](assets/isolation-architecture.svg){ width="100%" }](assets/isolation-architecture.svg)
  <figcaption>The trust boundaries from the <a href="threat-model.md">threat model</a> (B1 to B5), rendered as one static picture. Open the image for a full-size view. Every component name matches the codebase.</figcaption>
</figure>

The wall itself is [gVisor](https://gvisor.dev/) (`runsc`), a user-space kernel
that gives the agent a full Linux syscall surface while the real host kernel stays
behind a seccomp-bounded, capability-dropped boundary. Sandboxes run with
`network=none`, a read-only rootfs, `no_new_privs`, and a non-root user namespace.
The only crossings are two host-owned unix sockets (the model proxy that holds
your provider key, and the opt-in egress broker that is deny-by-default) plus the
per-session encrypted queues (`inbound.db` bound read-only, `outbound.db`
append-only).

For the design rationale behind this posture, why the host is trusted and the agent
is not, and what gVisor buys over a plain container, see
[Why we run AI agents in gVisor](gvisor-deep-dive.md).

## What a compromised agent still cannot do

We do not just assert the wall holds. We attack it. The
[red-team escape harness](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape)
stands up the offline demo control-plane, engages a real per-session sandbox, and
then runs each attack below **from inside that sandbox**, as the sandbox's own
user, exactly the privilege a jailbroken agent would have. It asserts each one is
contained.

| # | Attack from inside the sandbox | What contains it | Boundary |
| --- | --- | --- | --- |
| 1 | Network egress: enumerate NICs, resolve DNS | `network=none`, so only loopback exists and a packet has nowhere to go | B1 |
| 2 | Host escape via the Docker Engine socket | The Engine socket is never bound into the sandbox | B1 |
| 3 | Sibling container breakout | No docker client and no socket, so the daemon is unreachable | B1 |
| 4 | Read arbitrary host filesystem paths | Host root is outside the sandbox mount namespace (read-only rootfs) | B1 |
| 5 | Self-modification: enable a new tool | The request is held at the gateway's mandatory human-approval floor | B3 |
| 6 | Cross-session key theft (master and sibling keys) | Per-session binds only; the master key and sealed store are never mounted | B1, B2 |

Every row is **contained** on the production gVisor posture. The self-modification
case (row 5) is the one worth dwelling on: an agent can *ask* to enable a new tool,
but it can never *apply* the change. The request lands on the encrypted outbound
queue, the host turns it into a change request, and the gateway holds it in
`GET /v1/changes/pending` until a human approves it. There is no bypass path.

The same harness runs as a
[continuous CI gate](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/sandbox-containment.yml)
that also carries a negative control: it deliberately weakens the sandbox and
asserts the harness catches the regression, so the gate can never go quietly blind.

!!! note "Honest scope: the laptop demo relaxes two things"
    The zero-credential demo runs the `runc` fallback for laptop friendliness, which
    shares the host kernel and (until the per-session bind fix, IRO-259, lands)
    binds the whole state directory. The harness prints those as tracked gaps
    rather than pretending they are closed. The network, Docker-socket,
    sibling-breakout, and gateway boundaries hold **identically** on both paths.
    See the [harness README](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape)
    for the full demo-versus-production accounting.

## What the wall costs

Isolation is not free, but the cost is bounded and predictable rather than a tax on
every operation. Numbers below are the profile you should expect; reproduce them
with the harness in [Performance and footprint](benchmarks.md).

| Dimension | Overhead versus a `runc` baseline |
| --- | --- |
| CPU-bound reasoning | Near-native, within a few percent. Work that stays in userspace barely touches the wall. |
| Memory per sandbox | A fixed additive cost, on the order of tens of MiB of RSS beyond the workload. |
| Sandbox start | A one-time additive cost of roughly a couple hundred milliseconds, per launch, not per request. |
| Syscall or I/O heavy bursts | The largest gap, often in the range of roughly 1.5x to 2.5x, because every syscall is mediated. This is the isolation you are buying. |
| Network throughput | Not applicable. Sandboxes run `network=none` with no NIC, so gVisor's weakest dimension is removed by design. |

The takeaway: agent reasoning runs near-native, per-sandbox memory is a roughly
fixed cost so host capacity scales linearly with agent count, and the one real
overhead sits on syscall-heavy bursts, which is exactly the mediation that makes
the box a box.

## Verify it yourself

<div class="grid cards" markdown>

-   :material-target: **[Run the red-team harness](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape)**

    One command, no credentials. It attacks a live sandbox from the inside and
    prints a PASS or FAIL table.

-   :material-shield-bug: **[Read the threat model](threat-model.md)**

    The full boundary-by-boundary STRIDE analysis behind the diagram, and what
    counts as a vulnerability.

-   :material-speedometer: **[Reproduce the benchmarks](benchmarks.md)**

    The measured overhead, the methodology, and how to run it on your own host.

-   :material-lock-check: **[Verify a release](release-runbook.md#4-how-to-verify-a-release-user-facing)**

    Every build is checksummed, keyless-signed with cosign, and carries
    build-provenance attestations.

</div>

For the invariants that hold across all of this, start at the
[Security and trust overview](security.md).
