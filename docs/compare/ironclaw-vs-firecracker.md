---
title: "IronClaw vs Firecracker for AI agent isolation"
description: "Firecracker is a minimal KVM microVM with a real separate guest kernel, a genuinely strong isolation wall. But a VMM is not an agent runtime. What egress, host-side keys, an approval gateway, and continuous proof still require."
---

# IronClaw vs Firecracker

Firecracker is an excellent isolation primitive. It is a minimal virtual machine monitor
that boots a lightweight microVM on KVM in around a hundred milliseconds, with a
deliberately tiny device model and a real, separate guest kernel. AWS Lambda and Fargate
run untrusted code on it for a reason: hardware-virtualized isolation is arguably a
stronger kernel boundary than syscall interception, because the guest talks to its own
kernel rather than a reimplementation of the host's ABI. If you are weighing Firecracker
for agent workloads, you have picked a serious wall. The question this page answers is
what sits around it.

We describe the microVM pattern here and do not assert the specifics of any deployment.
Firecracker documents its own security model and requirements, so verify those against
its docs. This page is about matching the tool to the job: a bare VMM primitive, or a
complete agent runtime built around a boundary you can prove.

## A microVM is a wall, not a runtime

Firecracker gives you one thing very well: a strong, hardware-virtualized boundary for a
guest. An agent runtime has to answer several more questions that Firecracker on its own
does not:

- **Egress.** Firecracker isolates the guest; it does not decide the agent should have no
  network. You wire TAP devices and rate limiters yourself, then enforce the policy.
- **Credentials.** The provider API key has to reach the model somehow. If it lives inside
  the microVM as an env var, a compromised agent can read it, hardware isolation or not.
- **Approval.** Nothing in a VMM holds a new capability, tool, or egress rule for a human
  to approve before it takes effect.
- **Lifecycle.** One sandbox per conversation, sealed compiled-binary runtime, rootfs image
  management, the chat and channel plumbing, provider routing. That is the runtime, not
  the hypervisor.
- **Proof.** A boundary you cannot re-test on every change is a boundary you are hoping
  still holds.

## Firecracker alone vs the full stack

| Layer | Firecracker (bare microVM) | IronClaw |
| --- | --- | --- |
| Kernel isolation | Strong: separate guest kernel on KVM | Strong: gVisor user-space kernel, `network=none`, read-only rootfs |
| Host requirement | Bare-metal or nested KVM (`/dev/kvm`) | Linux + gVisor; macOS/Windows fall back to runc in Docker Desktop's VM |
| `network=none` enforced by default | You build TAP networking, then lock it down | Default on the sandbox |
| Provider key kept out of the box | Not addressed | Host-side proxy injects the key over a Unix socket |
| Human-approval gateway for capability changes | Not addressed | Built in, no bypass path |
| Per-conversation sandbox and sealed runtime | You build it | Default |
| Continuous proof the boundary holds | You script it | Red-team containment gate on every push |
| Supply-chain attestation of the runtime | Your responsibility | cosign, SLSA provenance, SBOMs |

## When Firecracker alone is the right call

Pick a bare microVM when you want maximum kernel isolation, you run on bare metal or a
KVM-capable host, and you are prepared to build the agent runtime around it: egress
policy, secret handling, approval, lifecycle, and orchestration. If you are running a
single well-understood workload at scale and already own that surrounding machinery, a
VMM primitive by itself may be exactly right, and adding a runtime you do not need is its
own cost. IronClaw does not compete for that.

## When to reach for IronClaw

Reach for IronClaw when you want a strong sandbox boundary *plus* the rest of the agent
runtime designed to the same standard, without hand-rolling egress, credential handling,
approvals, and containment tests around a bare hypervisor. IronClaw's distinctive posture
is **proof of containment, not a promise of it**: the isolation boundary is open source
and re-tested by a red-team escape suite on every push, so you inspect the wall rather
than trust a description. The provider key never enters the box, every conversation gets
its own sandbox with `network=none` and a read-only rootfs, and a human-approval gateway
guards capability changes with no bypass path.

IronClaw ships gVisor as its default boundary rather than a microVM, which trades a small
amount of raw isolation strength for no KVM requirement and a lighter operational surface.
The design point is not that gVisor beats a real VM; it is that the whole runtime around
the wall is built and verified to the same standard. See the reproducible
[sandbox containment benchmark](sandbox-containment-benchmark.md) for where different
isolation models land against a fixed escape suite.

## Confirm the posture locally

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

No `/dev/kvm` or nested virtualization required to try it.

## Where to go next

- The reproducible head-to-head: [sandbox containment benchmark](sandbox-containment-benchmark.md).
- Why a user-space kernel wall: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- The measured overhead: [Performance and footprint](../benchmarks.md).
- The full alternative rundown: [Why IronClaw](../comparison.md).
