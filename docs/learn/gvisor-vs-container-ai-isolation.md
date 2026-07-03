---
title: "gVisor vs containers for AI agent isolation"
description: "What a user-space kernel (gVisor runsc) buys you over a standard container when isolating untrusted AI agents, what it does not, and why to run it as a second wall rather than the only one."
---

# gVisor vs containers for AI agent isolation

If you are sandboxing an AI agent you assume can be compromised, the interesting
question about your container is: **what happens the moment the agent gets code
execution inside it?** That is where the difference between a standard container and a
gVisor-backed one stops being academic.

## The shared-kernel problem

A normal container is process isolation on top of the host kernel. Namespaces and
cgroups fence off what the process can *see*, but every syscall it makes runs against
the real host kernel directly. So the isolation is only as strong as the millions of
lines of syscall-handling code in that kernel: one kernel-level vulnerability is a
container-escape vulnerability. For a workload you are actively assuming is hostile, that
is a large, shared attack surface sitting right under the box.

## What gVisor changes

gVisor (`runsc`) interposes a user-space kernel, written in Go, between the sandboxed
process and the host. The agent's syscalls hit gVisor's reimplementation of the Linux
ABI, not the host kernel directly. The host kernel surface the agent can actually reach
shrinks to a small, audited interface. For the specific threat we care about, "the agent
breaks out of its box and onto the host," an attacker now has to defeat a **second
independent kernel** after they have already defeated the model. That is exactly the
right place to spend defense.

## Two honest caveats

Overclaiming is its own security smell, so:

- **gVisor is not a VM, and it has its own attack surface.** It is a strong additional
  isolation layer, not a magic one. Treat it as one wall among several, never the only
  one.
- **gVisor is Linux-only.** The production sandbox runtime `runsc` runs on Linux. On
  macOS the host side runs natively but a real agent sandbox falls back to runc inside
  Docker Desktop's Linux VM, a weaker, kernel-shared boundary. The laptop demo is for
  seeing it work; the sealed production posture is Linux plus gVisor.

## Defense in depth, not a single wall

This is the key design point: gVisor is not a substitute for the other controls, it is
an addition *underneath* them. In IronClaw the sandbox keeps `network=none`, a read-only
sealed rootfs, dropped capabilities, and host-side secrets in place, and gVisor is the
second kernel wall behind all of that. If any one layer fails, the others still hold. A
container-plus-LLM setup that relies on the shared host kernel alone has no such second
line. See [How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md) for the full set of
edges, and [Why we run AI agents in gVisor](../gvisor-deep-dive.md) for the deep dive.

## Verify the overhead, do not guess it

A second kernel is not free, so the cost should be measured, not asserted. IronClaw runs
`runsc` overhead benchmarks on Linux CI and publishes the numbers in
[Performance and footprint](../benchmarks.md), and the isolation boundary itself is
exercised on every push by a red-team containment gate. You can start the runtime and
confirm the posture locally:

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

## Where to go next

- The full isolation model: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- The controls that sit under gVisor: [How to sandbox an AI agent](how-to-sandbox-an-ai-agent.md).
- The measured cost: [Performance and footprint](../benchmarks.md).
- How IronClaw compares to raw container glue: [comparison](../comparison.md).
- Run it with your model: [model providers](../providers/index.md).
