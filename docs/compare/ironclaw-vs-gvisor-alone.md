---
title: "IronClaw vs gVisor alone for AI agents"
description: "gVisor (runsc) is a strong syscall-level wall, but a wall is not a runtime. What the rest of the agent lifecycle needs: egress control, host-side keys, an approval gateway, and proof."
---

# IronClaw vs gVisor alone

gVisor is excellent, and IronClaw uses it. `runsc` interposes a user-space kernel
between a sandboxed process and the host, so the agent's syscalls hit gVisor's
reimplementation of the Linux ABI instead of the real kernel. That shrinks the host
attack surface to a small audited interface. If you have already reached for gVisor to
run untrusted agents, you have made a good call. The question this page answers is what
sits around that wall.

## A wall is not a runtime

gVisor gives you one thing very well: a strong kernel boundary for a process. An agent
runtime has to answer several more questions that `runsc` on its own does not:

- **Egress.** gVisor sandboxes the process; it does not decide the agent should have no
  network. You still have to enforce `network=none` and the rest of the network policy.
- **Credentials.** The provider API key has to reach the model somehow. If it lives
  inside the sandbox as an env var, a compromised agent can read it, gVisor or not.
- **Approval.** Nothing in `runsc` holds a new capability, tool, or egress rule for a
  human to approve before it takes effect.
- **Lifecycle.** One sandbox per conversation, sealed compiled-binary runtime, the chat
  and channel plumbing, provider routing. That is the runtime, not the wall.
- **Proof.** A boundary you cannot re-test on every change is a boundary you are hoping
  still holds.

## gVisor alone vs the full stack

| Layer | gVisor (`runsc`) alone | IronClaw |
| --- | --- | --- |
| Kernel isolation | Yes, this is exactly what it does | Yes, IronClaw runs on gVisor |
| `network=none` enforced by default | You configure it | Default on the sandbox |
| Provider key kept out of the box | Not addressed | Host-side proxy injects the key over a Unix socket |
| Human-approval gateway for capability changes | Not addressed | Built in, no bypass path |
| Per-conversation sandbox and sealed runtime | You build it | Default |
| Continuous proof the boundary holds | You script it | Red-team containment gate on every push |
| Supply-chain attestation of the runtime | Your responsibility | cosign, SLSA provenance, SBOMs |

## When gVisor alone is enough

If you are running a single well-understood workload and you have already built egress,
secrets, approval, and lifecycle around it, gVisor by itself may be all you need. The
wall is genuinely strong, and adding a runtime you do not need is its own cost.

## When to reach for IronClaw

Reach for IronClaw when you want gVisor as the second kernel wall *plus* the rest of the
agent runtime designed to the same standard, so you are not hand-rolling egress,
credential handling, approvals, and containment tests around a bare `runsc`. IronClaw
treats gVisor as one wall among several, never the only one, and keeps `network=none`, a
read-only rootfs, dropped capabilities, and host-side secrets in place behind it.

## Confirm the posture locally

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

Note gVisor is Linux-only. On macOS the host runs natively but the sandbox falls back to
runc inside Docker Desktop's Linux VM, a weaker kernel-shared boundary. The sealed
production posture is Linux plus gVisor.

## Where to go next

- The deep dive: [Why we run AI agents in gVisor](../gvisor-deep-dive.md).
- Wall vs container: [gVisor vs containers for AI isolation](../learn/gvisor-vs-container-ai-isolation.md).
- The measured overhead: [Performance and footprint](../benchmarks.md).
- Run it with your model: [model providers](../providers/index.md).
