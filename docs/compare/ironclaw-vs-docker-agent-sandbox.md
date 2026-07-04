---
title: "IronClaw vs raw Docker for AI agent isolation"
description: "A plain Docker container is a good first boundary for an AI agent but it shares the host kernel and stops short of egress, credential, and approval control. What IronClaw adds on top."
---

# IronClaw vs raw Docker for agent isolation

Putting the agent in a container is the right instinct. It is a real boundary and a big
improvement over running the model's tools directly on your host. The gap is that a
container is process isolation on a **shared kernel**, and an agent runtime needs more
than process isolation.

## Where a plain container stops

`docker run` fences off what a process can see with namespaces and cgroups, but every
syscall the agent makes still runs against the real host kernel. So the isolation is
only as strong as the host's syscall-handling code: one kernel-level vulnerability is a
container-escape vulnerability. For a workload you are actively assuming is hostile,
that is a large shared attack surface sitting right under the box. And a bare container
says nothing about egress, where your keys live, or who approves a new capability. Those
are the parts you would still have to design and maintain yourself.

## What you build vs what ships

| Concern | Raw Docker + LLM glue | IronClaw |
| --- | --- | --- |
| Kernel isolation | Shared host kernel | gVisor (`runsc`) second kernel wall on Linux, container underneath |
| Network egress | Open by default, you lock it down | `network=none` on the sandbox by default |
| Filesystem | You configure mounts and read-only flags | Read-only sealed rootfs, one sandbox per conversation |
| Provider key | Usually passed in as an env var | Never enters the box, injected host-side over a Unix socket |
| Capability changes | Agent acts directly | Held at a human-approval gateway, no bypass path |
| Proof it holds | Your own review | Red-team containment gate on every push, published threat model |
| Supply chain of the runtime | Your image, your provenance | cosign signatures, SLSA provenance, SPDX + CycloneDX SBOMs |

## When raw Docker is the right call

If you want full control of every layer and are willing to own the security boundary,
DIY container plus LLM glue is a legitimate path. Teams pick it when they have the
security engineering time to design egress, secrets, and isolation themselves and want
no framework opinion in the way. IronClaw is not trying to talk you out of that; it is
what you would end up building if you kept hardening that container for agent workloads.

## When to reach for IronClaw

Reach for IronClaw when you want the hardened result without assembling and maintaining
it: a kernel-level sandbox, egress and credential control, and an approval gateway that
ship as the default and are exercised in CI. You keep the self-hosted, own-your-stack
posture of raw Docker and skip the part where a missed flag becomes a host compromise.

## Try it against your own container mental model

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

## Where to go next

- The kernel angle in depth: [gVisor vs containers for AI isolation](../learn/gvisor-vs-container-ai-isolation.md).
- The full control set: [How to sandbox an AI agent](../learn/how-to-sandbox-an-ai-agent.md).
- The evidence-backed rundown: [Why IronClaw](../comparison.md).
- Run it with your model: [model providers](../providers/index.md).
