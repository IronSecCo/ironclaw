---
title: "IronClaw vs Kata Containers for AI agent isolation"
description: "Kata Containers wraps each container in a lightweight VM for VM-grade isolation with container ergonomics. But an OCI runtime is not an agent runtime. What egress, host-side keys, an approval gateway, and continuous proof still require."
---

# IronClaw vs Kata Containers

Kata Containers is a strong isolation primitive with excellent ergonomics. It is an
OCI-compatible runtime that runs each container inside its own lightweight virtual machine
(backed by QEMU, Cloud Hypervisor, or Firecracker), so you get a real separate guest
kernel while keeping the container workflow: it drops into containerd or CRI-O and
Kubernetes as a runtime class. That gives you VM-grade isolation without rewriting how you
ship containers. If you are weighing Kata for agent workloads, you have picked a serious
wall. The question this page answers is what sits around it.

We describe the VM-per-container pattern here and do not assert the specifics of any
deployment. Kata documents its own security model and requirements, so verify those
against its docs. This page is about matching the tool to the job: an isolation runtime
you drop under your containers, or a complete agent runtime built around a boundary you
can prove.

## A VM-per-container is a wall, not a runtime

Kata gives you one thing very well: a strong, VM-backed boundary for a container. An agent
runtime has to answer several more questions that Kata on its own does not:

- **Egress.** Kata isolates the container in a VM; it does not decide the agent should have
  no network. You still define and enforce the network policy on top.
- **Credentials.** The provider API key has to reach the model somehow. If it lives inside
  the guest as an env var, a compromised agent can read it, VM boundary or not.
- **Approval.** Nothing in an OCI runtime holds a new capability, tool, or egress rule for a
  human to approve before it takes effect.
- **Lifecycle.** One sandbox per conversation, sealed compiled-binary runtime, image and
  channel plumbing, provider routing. That is the runtime, not the runtime class.
- **Proof.** A boundary you cannot re-test on every change is a boundary you are hoping
  still holds.

## Kata alone vs the full stack

| Layer | Kata Containers (bare runtime) | IronClaw |
| --- | --- | --- |
| Kernel isolation | Strong: separate guest kernel per container VM | Strong: gVisor user-space kernel, `network=none`, read-only rootfs |
| Host requirement | Bare-metal or nested KVM for the VMM backend | Linux + gVisor; macOS/Windows fall back to runc in Docker Desktop's VM |
| Fits existing container tooling | Yes, drops in as an OCI runtime class | Runs as its own runtime; uses OCI/gVisor under the hood |
| `network=none` enforced by default | You configure it in the pod/container spec | Default on the sandbox |
| Provider key kept out of the box | Not addressed | Host-side proxy injects the key over a Unix socket |
| Human-approval gateway for capability changes | Not addressed | Built in, no bypass path |
| Per-conversation sandbox and sealed runtime | You build it | Default |
| Continuous proof the boundary holds | You script it | Red-team containment gate on every push |
| Supply-chain attestation of the runtime | Your responsibility | cosign, SLSA provenance, SBOMs |

## When Kata alone is the right call

Pick Kata when you already run a container platform, want VM-grade isolation under your
existing orchestration, and are prepared to build the agent runtime around it: egress
policy, secret handling, approval, and lifecycle. If your workloads are general containers
and the sandbox is a platform concern rather than an agent-specific security control, a
drop-in OCI runtime is often the cleaner fit, and adding an agent runtime you do not need
is its own cost. IronClaw does not compete for that.

## When to reach for IronClaw

Reach for IronClaw when you want a strong sandbox boundary *plus* the rest of the agent
runtime designed to the same standard, without hand-rolling egress, credential handling,
approvals, and containment tests around a bare OCI runtime. IronClaw's distinctive posture
is **proof of containment, not a promise of it**: the isolation boundary is open source and
re-tested by a red-team escape suite on every push, so you inspect the wall rather than
trust a description. The provider key never enters the box, every conversation gets its own
sandbox with `network=none` and a read-only rootfs, and a human-approval gateway guards
capability changes with no bypass path.

IronClaw ships gVisor as its default boundary rather than a per-container VM, which trades
a small amount of raw isolation strength for no KVM requirement and a lighter operational
surface, then builds the whole agent runtime around it to the same standard. See the
reproducible [sandbox containment benchmark](sandbox-containment-benchmark.md) for where
different isolation models land against a fixed escape suite.

## Confirm the posture locally

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

No nested-virtualization host or runtime class to install to try it.

## Where to go next

- The reproducible head-to-head: [sandbox containment benchmark](sandbox-containment-benchmark.md).
- Wall vs container: [gVisor vs containers for AI isolation](../learn/gvisor-vs-container-ai-isolation.md).
- The measured overhead: [Performance and footprint](../benchmarks.md).
- The full alternative rundown: [Why IronClaw](../comparison.md).
