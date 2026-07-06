---
title: "IronClaw vs Modal Sandboxes for AI agents"
description: "Modal runs agent sandboxes as a serverless cloud you rent. IronClaw is the self-hosted alternative that keeps the sandbox, your keys, and your data on infrastructure you own. When each side wins."
---

# IronClaw vs Modal Sandboxes

Modal is a serverless cloud for AI and data workloads, and its Sandboxes let you run
untrusted or agent-generated code in an isolated container the platform spins up for you.
It solves a real problem well: you call an API, code runs in a strong isolated environment,
and you never operate a cluster. The trade is architectural, not a knock on the product.
Modal runs that sandbox on **its** infrastructure, and in most agent setups the keys and
data flowing through the agent pass through that managed environment. IronClaw is the
self-hosted answer to the same job.

We describe the common managed-cloud pattern here and do not assert the specifics of any
named platform. Modal, for example, documents its own isolation approach, so verify those
details against Modal's docs. This page is about the managed-cloud-vs-self-hosted trade so
you can pick the side that fits your constraints.

## The trade, honestly

| Axis | Modal Sandboxes (managed cloud) | IronClaw (self-hosted) |
| --- | --- | --- |
| Ops burden | None, Modal runs it | You run it, self-hosted |
| Where sandboxes execute | On Modal's cloud | On your infrastructure |
| Where your data lives | On the platform's infrastructure | On yours |
| Where provider keys live | Commonly with the platform or passed through it | Host-side on your box, never inside the sandbox |
| Isolation you can inspect | Platform internal detail you verify from their docs | gVisor + `network=none` + read-only rootfs, open source |
| Agent lifecycle (approval, per-conversation) | You build the agent runtime on top | Built in: one sandbox per conversation, human-approval gateway |
| Data residency and compliance | Depends on the platform's regions and terms | Fully under your control |
| Cost model | Usage-based, per second of compute | Your infrastructure, AGPLv3 + commercial |
| Lock-in | Platform API and runtime | Own the stack, portable |

## When Modal wins

Pick a managed serverless sandbox when zero ops and elastic scale matter most: spiky or
bursty workloads, GPU jobs you do not want to run yourself, prototyping, or a team with no
platform engineers. If your data and keys are comfortable living with a third party and
usage-based pricing beats owning infrastructure, a hosted platform is often the faster and
cheaper call. That is a legitimate choice and IronClaw does not compete for it.

## When to reach for IronClaw

Reach for IronClaw when the answer to "who holds our keys and data" has to be "we do."
Modal gives you a strong sandbox primitive; IronClaw gives you a **complete self-hosted
agent runtime** built around a boundary you can inspect: the provider key never enters the
box (a host-side proxy injects it and makes the outbound call), every conversation gets its
own gVisor-backed sandbox with `network=none` and a read-only rootfs, and a human-approval
gateway sits in front of capability changes with no bypass path. The isolation is open
source and re-tested by a red-team containment gate on every push, so a security team can
prove it rather than trust a description.

## Run the self-hosted path in minutes

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

## Where to go next

- The full alternative rundown, including hosted platforms: [Why IronClaw](../comparison.md).
- The other managed option, side by side: [IronClaw vs hosted sandboxes (E2B)](ironclaw-vs-e2b-hosted-sandboxes.md).
- What self-hosting buys you at the boundary: [How to sandbox an AI agent](../learn/how-to-sandbox-an-ai-agent.md).
- From clone to held approval: [quickstart](../quickstart.md).
