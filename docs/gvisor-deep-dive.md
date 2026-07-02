---
title: "Why we run AI agents in gVisor"
description: "The security model behind IronClaw: why a sandboxed agent gets no network, no host secrets, and no way to change its own configuration, and what gVisor actually buys you."
---
# Why we run AI agents in gVisor

Give an AI agent tools, and you've given it a way to act on the world: read a file,
call an API, send a message, spend money. Give it a chat inbox, and you've given an
**attacker** a way to steer those actions, a poisoned web page, a booby-trapped
email, a hostile tool result. Prompt injection is not a corner case for an
autonomous agent; it is the normal operating condition.

So at IronClaw we start from an uncomfortable assumption and design backwards from
it:

> **Assume the agent is already compromised.** Treat every model output, every
> inbound message, and every byte returned by an external API as hostile. Then build
> walls that hold anyway.[^assume]

That single asymmetry, *the host is trusted, the agent is not*, is the whole
design. This post is about the most load-bearing wall it produces: running each
agent inside a **gVisor** sandbox with **no network**, and what that actually buys
you.

## The thing most agent frameworks get wrong

The dominant pattern for "autonomous agent" today is a loop with a shell tool,
running as your user, on your machine, with your network and your credentials. It
works beautifully right up until the model is talked into `curl | sh`-ing something,
reading `~/.ssh`, or POSTing your environment variables to an attacker. The agent
didn't "go rogue", it did exactly what the injected instructions said, with exactly
the privileges you handed it.

You cannot prompt-engineer your way out of this. A system prompt that says "never
exfiltrate secrets" is a request, not a boundary. The fix has to live *below* the
model, where the model's output can't reach it.

## What "below the model" looks like

In IronClaw, every agent session runs in its own sandbox container, and the sandbox
is sealed along a small number of deliberately boring edges:[^b1]

- **`network=none`.** The sandbox has no network namespace, no NIC, no DNS, no
  routes. It cannot open a socket to the internet because there is nothing to open.
  It reaches the model **only** through a host-owned Unix socket (a model proxy), and
  it reaches approved external APIs **only** through a second host-owned socket (an
  egress broker). Every byte that leaves crosses a host choke point.[^netnone]
- **Read-only, sealed runtime.** The rootfs is read-only; mounts are
  `nosuid,nodev,noexec`; capabilities are dropped; `no_new_privs` is set; the process
  runs in a non-root user namespace. There is no interpreter and no source in the box
  to rewrite, it's a compiled Go binary.[^sealed]
- **Encrypted, per-session queues.** The agent talks to the outside world by reading
  an inbound queue (read-only to it) and appending to an outbound queue. Each
  session's queues are separate, encrypted SQLite databases with their own 256-bit
  key. One session physically cannot read another's data, and nothing sits in
  plaintext on disk.[^queues]

And then the part people remember: that sandbox runs under **gVisor (`runsc`)**.

## What gVisor adds, and what it doesn't

`network=none` plus a read-only rootfs is already a strong box. gVisor hardens the
one wall those controls *don't* directly cover: the **kernel** boundary.

A normal container shares the host kernel. Every syscall the containerized process
makes runs against the real kernel directly, so a kernel-level vulnerability is a
container-escape vulnerability, the isolation is only as strong as the millions of
lines of syscall-handling code in the host kernel. gVisor interposes a user-space
kernel (written in Go) between the sandboxed process and the host: the agent's
syscalls hit gVisor's reimplementation of the Linux ABI, not the host kernel
directly. The host kernel surface the agent can actually touch shrinks to a small,
audited interface.

For an agent we are *assuming is compromised*, that is exactly the right place to
spend defense. The threat we care about, "the agent finds a way to break out of its
box and onto the host", is precisely a sandbox-escape threat, and a second
independent kernel is a second thing an attacker has to defeat after they've already
defeated the model.[^escape]

Two honest caveats, because overclaiming is its own security smell:

- **gVisor is not a VM, and it has its own attack surface.** It is a strong
  additional isolation layer, not a magic one. We treat it as the trusted runtime
  (it's inside the trust boundary), and we keep `network=none` and the sealed rootfs
  in place *underneath* it so that gVisor is the second wall, never the only one.
- **gVisor is Linux-only.** The production sandbox runtime is `runsc`, which only
  runs on Linux. The entire IronClaw host side runs natively on macOS, but a real
  agent sandbox there falls back to runc inside Docker Desktop's Linux VM, a
  weaker, kernel-shared boundary. The laptop demo is for *seeing it work*; the sealed
  production posture is Linux + gVisor.[^platform]

> **This is not just a design claim.** The isolation boundary is exercised on every push by a red-team containment gate (`.github/workflows/sandbox-containment.yml`), and the `runsc` overhead is measured on Linux CI in [Performance and footprint](benchmarks.md). You can reproduce the escape attempts yourself: see [Breaking our own sandbox](breaking-our-own-sandbox.md).

## The wall the model never sees: the approval gateway

Isolation answers "what can a compromised agent reach?" It doesn't answer "what can a
compromised agent *change about itself*?", and for a long-running agent that second
question matters just as much. An agent that can quietly grant itself a new tool, a
new egress destination, or a new mount has escalated, even without escaping the
sandbox.

So configuration changes don't go through the model's hands at all. **An agent cannot
change its own configuration.** Every capability change, persona, tools, packages,
wiring, permissions, mounts, even provisioning a brand-new agent, is held at a
deterministic **gateway** for a human decision. Deny-by-default, with a mandatory
human-approval floor that no change kind is allowed to skip.[^gateway]

The agent can *ask* ("I'd like access to this MCP server / this skill / this egress
host"). Only a human can *grant*. The ask is visible, the approver sees exactly the
capabilities being requested in the change diff, so a trojaned skill that quietly
requests `egress: evil.example.com` is *visible and rejected at review time*, not
discovered post-breach.[^skills] The same floor governs everything extensible:
skills are **data, not code** (they declare grants; they can never ship a script or
self-install)[^skills], and MCP servers stay entirely host-side and gateway-gated, so
they add tool reach without adding a boundary the sandbox can cross.[^mcp]

## What this buys you, concretely

Put the layers together and you get a property worth stating plainly. Even a
**fully compromised agent**, one doing exactly what an attacker's injected prompt
tells it to, cannot:

- read another session's conversation data, or any at-rest plaintext;[^queues]
- reach the host filesystem, or any network host that isn't on an
  operator-approved egress allowlist;[^netnone]
- obtain a host-held secret (the model API key, the queue keys, the API token), 
  the model proxy injects the key into the outbound call host-side, so the key
  never enters the sandbox;[^secrets]
- change its own configuration or grant itself a new capability without a human
  approving it at the gateway.[^gateway]

That is the difference between "we asked the model nicely" and "the boundary holds
even when the model is hostile." It's also why we publish a full
[threat model](threat-model.md), adversaries, trust boundaries, a STRIDE pass per
boundary, and an explicit list of what *is* and *isn't* a vulnerability, and version
it with the code. A security claim you can't audit is a marketing claim.

## Try the boundary yourself

The fastest way to believe a security model is to watch it refuse something. The
[quickstart](quickstart.md) walks you from a clean clone to **submitting a
configuration change and watching it get held at the gateway** until you approve it, 
no model key required, entirely on your machine. Five minutes, and the core invariant
stops being a paragraph and becomes a command you ran.

---

### Accuracy notes (claim → source)

Every claim above maps to a versioned source in the repository. This post makes **no**
claim that is not backed by shipped code or the versioned threat model, so you can
check each one for yourself.

[^assume]: Core assumption and trust asymmetry, [threat model §1](threat-model.md).
[^b1]: Trust boundaries B1-B5 and data-flow diagram, [threat model §3](threat-model.md).
[^netnone]: `network=none`, host model-proxy socket, egress broker, deny-by-default allowlist, [threat model §3, §5 (B1/B4), §7](threat-model.md); [security posture](security.md).
[^sealed]: Read-only rootfs, dropped caps, `no_new_privs`, non-root user namespace, compiled binary / no interpreter, [threat model §5 (B1, rows T and E)](threat-model.md); [security posture](security.md).
[^queues]: Per-session encrypted SQLite queues, read-only inbound / append-only outbound, cross-session isolation, [threat model §5 (B2)](threat-model.md).
[^escape]: gVisor (`runsc`) as the sandbox-escape mitigation for boundary B1 (row E), [threat model §5 (B1)](threat-model.md).
[^platform]: gVisor is Linux-only; macOS falls back to runc-in-Docker, [README → Platform support](https://github.com/IronSecCo/ironclaw#platform-support); [quickstart security note](quickstart.md).
[^gateway]: Mandatory human-approval gateway, `AlwaysRequireHuman` floor, agent cannot change its own config, [threat model §5-§6](threat-model.md); [security posture](security.md); [quickstart "Your first approved action"](quickstart.md).
[^secrets]: Host-held secrets never enter the sandbox; model proxy injects the key host-side, [threat model §2, §5 (B1, row I), §6](threat-model.md).
[^skills]: Skills are gateway-gated capability bundles, data-not-code, signature-verified, fail-closed, [threat model §12](threat-model.md); [skills](skills.md).
[^mcp]: MCP servers are host-side, gateway-gated, deny-by-default, isolated, [threat model §13](threat-model.md).
