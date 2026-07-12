---
title: "gVisor vs runc: container isolation compared, with escape-suite numbers"
description: "gVisor and runc score identically on a config scan, yet block a different number of real escape attempts. runc (default) blocked 2 of 5, hardened runc blocked 4 of 5, gVisor blocked 5 of 5. The gap is one attempt: the shared host kernel. Here is when hardened runc is enough and when you need a user-space kernel, backed by a re-runnable benchmark."
---

# gVisor vs runc: container isolation compared

If you are choosing a container runtime for untrusted or AI-generated code, the
question is narrow and important: **does gVisor actually contain more than runc, and
by how much?** Two things surprised us when we measured it, and both matter for the
decision.

The first: on a configuration scan, gVisor and runc score the *same*. The second:
against a live escape suite, they do not. Understanding why is the whole answer.

## Same config, same score

[`ironctl scan`](audit-your-sandbox-in-10-seconds.md) grades a container's
configuration posture: capabilities, user, filesystem, egress, seccomp. It is
deliberately runtime-agnostic. Run the same hardened container under runc and under
gVisor and you get the identical 100 out of 100, because the *configuration* is
identical. The scan cannot see the runtime underneath.

That is not a gap in the scan. It is the point. A config scanner tells you whether
you closed the holes you control with flags. It cannot tell you whether the kernel
serving your syscalls is the host's. For that you have to actually attack the box.

## Different runtime, different containment

So we did. We built one fixed escape-attempt suite, ran it from *inside* each
runtime posture, and counted what got through. Full method and per-attempt
breakdown are in the
[containment benchmark](containment-benchmark-docker-gvisor-e2b-daytona.md); the
head-to-head is this:

| Runtime posture | Escape attempts | Blocked | Block rate |
| --- | ---: | ---: | ---: |
| runc, default | 5 | 2 | 40% |
| runc, hardened | 5 | 4 | 80% |
| gVisor / runsc | 5 | 5 | 100% |

Hardening runc with dropped capabilities, `--network=none`, and a read-only rootfs
takes you from 40% to 80%. That is real and worth doing. But there is one attempt it
never blocks, no matter how many flags you add:

| Escape attempt | runc hardened | gVisor |
| --- | :---: | :---: |
| No network interface | BLOCKED | BLOCKED |
| No outbound egress | BLOCKED | BLOCKED |
| Host root not mounted | BLOCKED | BLOCKED |
| Privileged syscalls dropped | BLOCKED | BLOCKED |
| **Host kernel not reached** | **OPEN** | **BLOCKED** |

## The one line that separates them

Under runc, every syscall a compromised process makes is served by the **host
kernel**. Hardening removes privileges, but the process is still talking directly to
your kernel, so its full syscall and CVE surface is one bug away. That is the
`kernel.shared-host` attempt, and it stays OPEN under runc at any hardening level.

Under gVisor, a user-space kernel called the Sentry sits in the path and serves those
syscalls itself. The host kernel is never the thing the workload talks to. That is
the row where the last attempt finally flips to BLOCKED, and it is the entire reason
gVisor exists.

## Which one do you need?

Runtime choice is a threat-model choice, not a "more is better" choice.

- **Hardened runc (80%) is enough when** you trust the code you run and want strong
  defense in depth against a container that gets popped. Dropped caps, no egress, and
  a read-only rootfs are a genuine boundary. Most production workloads live here, and
  they should be hardened, not run on defaults.
- **gVisor (100%) is what you want when** the code is untrusted by design: AI agents
  executing generated code, multi-tenant sandboxes, anything where you must assume the
  workload is already hostile. There, the shared host kernel is not an acceptable
  single point of failure, and a user-space kernel is the thing that closes it.

The cost of gVisor is a syscall-performance overhead and some compatibility edges.
The benefit is that the last escape attempt has nowhere to go. For an autonomous
agent running arbitrary code, that trade is the whole reason
[IronClaw](https://github.com/IronSecCo/ironclaw) defaults its runtime to gVisor.

## Verify it yourself

The benchmark is a committed script that runs in CI, so the numbers cannot quietly
drift. Re-run the
[containment-matrix harness](https://github.com/IronSecCo/ironclaw/blob/main/scripts/bench/containment-matrix.sh)
and check the block rates against this post. And whatever runtime you land on, scan
your container's configuration so the flags you *do* control are not the weak link:

```bash
ironctl scan your-image:tag
```

Browse the full 151-image isolation ranking and grab a score badge for your repo at
the **[Container Isolation Scores explorer](https://nivardsec.com/scores)**.
