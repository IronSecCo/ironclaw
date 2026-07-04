---
title: "IronClaw vs hosted agent sandboxes (E2B and similar)"
description: "Hosted agent sandboxes trade ops for a third party holding your keys and data. IronClaw is the self-hosted alternative that keeps both on your infrastructure. When each side wins."
---

# IronClaw vs hosted agent sandboxes

Hosted agent sandboxes (E2B and similar managed platforms) solve a real problem well:
you sign up, call an API, and someone else runs the isolated environment. No cluster to
operate, no runtime to patch. The trade is architectural, not a knock on any product:
in a hosted model, a third party runs the sandbox and, in most agent setups, holds the
keys and data flowing through it. IronClaw is the self-hosted answer to the same job.

We describe the common hosted pattern here and do not assert the specifics of any named
platform; verify those against that platform's own docs. This is about the
managed-vs-self-hosted trade, so you can pick the side that fits your constraints.

## The trade, honestly

| Axis | Hosted sandbox (managed) | IronClaw (self-hosted) |
| --- | --- | --- |
| Ops burden | None, the vendor runs it | You run it, self-hosted |
| Where your data lives | On the vendor's infrastructure | On yours |
| Where provider keys live | Commonly with the vendor or passed through it | Host-side on your box, never inside the sandbox |
| Isolation you can inspect | Vendor internal detail | gVisor + `network=none` + read-only rootfs, open source |
| Data residency and compliance | Depends on the vendor's regions and terms | Fully under your control |
| Approval gateway for capability changes | Varies by platform | Built in, no bypass path |
| Cost model | Usage-based, per sandbox | Your infrastructure, AGPLv3 + commercial |
| Lock-in | Vendor API and runtime | Own the stack, portable |

## When a hosted sandbox wins

Pick a managed sandbox when time-to-value and zero ops matter most, your data and keys
are comfortable living with a third party, and elastic usage-based scaling beats owning
infrastructure. For prototyping, spiky workloads, or teams with no platform engineers, a
hosted sandbox is often the faster and cheaper call. That is a legitimate choice and
IronClaw does not compete for it.

## When to reach for IronClaw

Reach for IronClaw when the answer to "who holds our keys and data" has to be "we do":
regulated data, strict residency requirements, secrets that cannot leave your network,
or a security team that needs to inspect and prove the isolation rather than trust a
vendor's description of it. IronClaw keeps the whole path on infrastructure you control,
with a boundary that is open source and re-tested by a red-team containment gate on
every push.

## Run the self-hosted path in minutes

```bash
docker compose -f docker-compose.demo.yml up --build -d
./bin/ironctl doctor   # reports the sandbox runtime and isolation posture
```

## Where to go next

- The full alternative rundown, including hosted platforms: [Why IronClaw](../comparison.md).
- What self-hosting buys you at the boundary: [How to sandbox an AI agent](../learn/how-to-sandbox-an-ai-agent.md).
- Bring your own model, local or cloud: [model providers](../providers/index.md).
- From clone to held approval: [quickstart](../quickstart.md).
