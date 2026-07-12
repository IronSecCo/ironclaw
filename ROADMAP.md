# IronClaw roadmap

This is the short, contributor-facing view: where IronClaw is, where it's headed,
and **where we'd love your help**. For the full status-at-a-glance tables and the
category comparison, the **living roadmap is on the docs site:
[Road to 1.0](https://ironsecco.github.io/ironclaw/roadmap/)** — that page is the
single source of truth and is kept in lockstep with what's actually shipped.

> Legend: ✅ done · 🚧 in progress · ⬜ planned

## Where IronClaw is today

The **security backend is complete** — gVisor/Kata isolation with `network=none`,
SQLCipher-encrypted per-session queues, a deterministic approval gateway with a
mandatory-human floor, a host-brokered egress broker, a credential vault, and
signed releases with an SBOM and build provenance. The remaining work to 1.0 is
**product surface and ecosystem**, not the core.

- ✅ Isolation (gVisor + Kata), encrypted queues, approval gateway, egress broker
- ✅ 12 channel adapters + an in-product web chat playground (13 delivery surfaces)
- ✅ Multi-provider models (Anthropic · OpenAI · OpenRouter · Codex · Gemini · Vertex · local OpenAI-compatible)
- ✅ Embedded mesh-only web console (approvals inbox, sessions, logs, chat playground)
- ✅ Signed releases + SBOM + provenance; threat model; OpenAPI spec; docs site
- ✅ Credential vault with logical-name `vault://` injection behind the gateway

## What "1.0" means

- **Public-ready** — meets every GitHub community standard and the category's
  onboarding bar.
- **At parity** — a web UI, broad channels, and guided setup: the product
  experience of the category, on IronClaw's stronger security base.
- **Best-in-class trust** — signed and reproducible builds, an SBOM, a published
  threat model, and a third-party security audit.

See the [docs roadmap](https://ironsecco.github.io/ironclaw/roadmap/) for the
wave-by-wave breakdown of what's done and what's left.

## Where we'd love help (help-wanted themes)

You don't need to touch the security core to make a real difference. These are the
areas where outside contributions land best:

- **Channel adapters** — they're small, uniform, and dependency-free. Adding or
  improving one is a great first PR. See
  [Writing a channel adapter](https://ironsecco.github.io/ironclaw/writing-a-channel-adapter/).
- **Docs & examples** — tutorials, cookbook recipes, and clarifications. The
  [examples gallery](https://ironsecco.github.io/ironclaw/examples/) always has room.
- **Test coverage** — hermetic unit and integration tests for host and sandbox
  packages; fuzz targets for parsers.
- **Developer experience** — onboarding friction, `ironctl` ergonomics, build and
  tooling polish.
- **Reproducible builds** — all three binaries (control-plane, `ironctl`, and
  `sandbox`) already reproduce bit-for-bit on Linux, same-runner and cross-machine.
  Help extend verified reproducibility to more platforms and independently
  rebuild-and-verify a published release against its `SHA256SUMS`.

The fastest way in is a
[**good first issue**](https://github.com/IronSecCo/ironclaw/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)
— small, self-contained, and mentored. Comment to claim one before you start, and
see [`CONTRIBUTING.md`](CONTRIBUTING.md) for the 5-minute quickstart. Got an idea
that isn't an issue yet? Open a thread in
[**Discussions**](https://github.com/IronSecCo/ironclaw/discussions).
