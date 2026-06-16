# IronClaw

**Security-first, self-hosted AI agents — isolation you can prove, not just promise.**

IronClaw is an open-source platform for running personal AI assistants on infrastructure you
control. You talk to them through the chat apps you already use; each assistant runs as a real,
autonomous agent that can read, write, schedule, and reply. What makes IronClaw different is the
threat model: it assumes the agent (and the box it runs in) could be compromised at any moment,
and builds hard, provable walls so that even a misbehaving agent can't reach your data or your
machine.

> **Status:** design / pre-alpha. The architecture is settled; the implementation skeleton is next.
> **License:** MIT.

## Why it's different

| Pillar | What it is | Attack surface it removes |
|--------|------------|----------------------------|
| **Sealed runtime** | The agent ships as a compiled Go binary | Agent self-modification — there's no source inside the box to rewrite |
| **Approved by humans** | Every change to the harness clears a deterministic gateway | Silent setting changes — nothing changes without a human seeing and approving it |
| **Encrypted queues** | Per-session encrypted message queues; read-only inbound | Data theft at rest, and cross-session reads |
| **Sealed sandbox** | gVisor container, no network, host-proxied model calls | Data exfiltration and sandbox escape |
| **Private control panel** | Admin access over a private mesh (Tailscale) only | Remote attacks on the controls |

The throughline: **treat the agent as untrusted, and make the security boundary something you can
verify — not something you take on faith.**

## How it works (in one breath)

Two compiled Go programs that never share memory and talk only through a pair of encrypted files
per conversation. A **control plane** (the always-on manager) receives chats, routes them, holds
the keys, and runs the approval gateway. A **sandboxed agent** — one per conversation, wrapped in
gVisor with no network of its own — reads its encrypted inbox (read-only), calls the AI model
through a host-controlled proxy, and writes its encrypted outbox. Every powerful action is something
the control plane does on the agent's behalf, after its own checks.

## What's in this repo

| Path | What it is |
|------|------------|
| `docs/ironclaw-explained.md` / `.pdf` / `.docx` | A plain-language tour of the design, with diagrams and a glossary |
| `docs/design-plan.md` | The architecture decisions and the implementation skeleton plan |
| `teaser.html` | A one-slide introduction deck |

## Roadmap

- [x] Architecture and threat model
- [x] Design walkthrough + introduction materials
- [ ] Compiling skeleton: frozen contract package, control-plane and sandbox stubs, CI
- [ ] Control plane (routing, gateway, isolation, key custody, delivery)
- [ ] Sandbox (agent loop, model provider, queue access)
- [ ] gVisor integration + behavioral test suite

## License

[MIT](LICENSE) © 2026 nivardsec
