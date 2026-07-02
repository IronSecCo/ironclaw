---
title: IronClaw public roadmap (road to 1.0)
description: A living view of where IronClaw is and where it is going. The security backend is complete; this page tracks the road to a 1.0 product, including a web UI, product parity, and public-launch readiness.
---

# IronClaw — public roadmap (Road to 1.0)

A living view of where IronClaw is and where it's going. The **security backend
(Waves 0–5) is complete**; this page tracks the road to a 1.0 *product* —
public-launch readiness, product parity (including a web UI), and best-in-class
supply-chain trust.

> Legend: ✅ done · 🚧 in progress · ⬜ planned · 👤 needs a maintainer decision.

<div align="center">

<img src="assets/demo.svg" width="800" alt="IronClaw quickstart terminal session: install the binaries, start the control-plane in dev mode on http://127.0.0.1:8787, submit a capability change that is held at the gateway pending human approval, then approve it.">

</div>

## Status at a glance

| Phase | Scope | Progress |
|-------|-------|----------|
| **Waves 0–5** | Security backend (isolation, encrypted queues, gateway, registry, channels, scheduling, egress, a2a) | ✅ **complete** |
| **Wave 6** | Public-launch readiness | 🚧 most of the way |
| **Wave 7** | Product parity + web UI | 🚧 channels done; web UI planned |
| **Wave 8** | Trust, supply-chain & ecosystem | 🚧 docs site, signed releases + SBOM, threat model, OpenAPI & examples done |

The backend is done; the remaining work is product surface, a UI, onboarding,
and open-source/supply-chain polish — **not** the security core.

## Wave 6 — Public-launch readiness

Everything needed to flip the repo public credibly.

| Item | Status |
|------|--------|
| `SECURITY.md` + private vulnerability reporting | ✅ |
| `CODE_OF_CONDUCT.md` | ✅ |
| Issue forms + PR template | ✅ |
| Launch-grade README (hero, demo, badges) | ✅ |
| Repo description + topics | ✅ |
| Social-preview image | 🚧 (asset ready; upload pending) |
| `docker-compose.yml` + `.env.example` + published image | ✅ |
| Guided `ironctl onboard` wizard | ✅ |
| 5-minute quickstart tutorial | ✅ |
| Homebrew tap + CHANGELOG + release notes | ✅ |
| Public-repo ruleset for push-to-main | ⬜ |
| Discussions + seeded good-first-issues | ✅ |
| Real-time community chat (Discord / Matrix) | 🚧 |

## Wave 7 — Product parity & web UI

The product surface that brings IronClaw level with the category, on its stronger
security base.

### Web UI

CLI- and API-first is a deliberate feature today (no public web surface to
attack). A private, **loopback/mesh-only** web console — reusing the API token, never
widening the network posture — ships embedded in the control-plane binary:

| Item | Status |
|------|--------|
| Web console architecture + scaffold | ✅ |
| Approvals inbox (the gateway in a browser) | ✅ |
| Sessions browser | ✅ |
| Channels & wiring management | ✅ |
| Logs & audit viewer | ✅ |
| Config editor + web onboarding wizard | ✅ |
| Chat playground | ✅ |

### Channels, persona & observability

| Item | Status |
|------|--------|
| Channel adapter: WhatsApp | ✅ |
| Channel adapter: Email / Gmail | ✅ |
| Channel adapter: Matrix | ✅ |
| Channel adapter: Google Chat | ✅ |
| Channel adapters: Microsoft Teams, Signal, iMessage | ✅ |
| First-class persona / identity surface | ✅ |
| Observability CLI (`ironctl status` / `doctor` / usage) | ✅ |
| Host-side skills / extension system | ✅ |
| Multi-provider model support | ✅ |

IronClaw now speaks Slack, Discord, Telegram, Microsoft Teams, Signal, iMessage,
Webhook, WhatsApp, Email/SMTP, Matrix, and Google Chat — plus the in-product web
chat playground, for twelve delivery surfaces in all.

## Wave 8 — Trust, supply-chain & ecosystem

Press the security advantage — several of these are wins neither peer has claimed.

| Item | Status |
|------|--------|
| Documentation site | ✅ (this site) |
| Checked-in OpenAPI spec | ✅ |
| Threat model — STRIDE + data-flow | ✅ |
| Signed releases + SBOM + provenance | ✅ |
| Supply-chain hygiene (Dependabot / CodeQL / secret scanning / pinned actions) | ✅ |
| OpenSSF Scorecard + Best-Practices badges | 🚧 (Scorecard workflow live) |
| Reproducible builds | 🚧 (`ironctl` / `sandbox` reproducible; control-plane tracked) |
| Examples gallery + templates | ✅ |
| Public roadmap + comparison (this page) | ✅ |
| Third-party security audit | 👤 |
| End-user credential vault | ✅ |

### What "1.0" means

- **Public-ready** (end of Wave 6): meets every GitHub community standard and the
  category's onboarding bar.
- **At parity** (end of Wave 7): a web UI, broad channels, and guided setup — the
  product experience of the category, on IronClaw's stronger security base.
- **Best-in-class trust** (end of Wave 8): signed/reproducible builds, an SBOM, a
  published threat model, and a third-party audit.

## How IronClaw compares

How we see IronClaw against the `claw` ecosystem — primarily
[`nanocoai/nanoclaw`](https://github.com/nanocoai/nanoclaw) (a lightweight,
container-isolated assistant) and [`openclaw/openclaw`](https://github.com/openclaw/openclaw)
(the category bar, which ships a full Control UI). This is IronClaw's own
positioning; peer capabilities are described from their public positioning and
will evolve — corrections welcome via an issue.

| Capability | nanoclaw / openclaw | IronClaw | Where IronClaw stands |
|---|---|---|---|
| Container isolation | Docker / opt-in host access | gVisor + `network=none` + Kata backend | ✅ **stronger** |
| Approval / permissions | role checks / host access | mandatory **deterministic gateway** with a human-approval floor | ✅ **stronger** |
| Encrypted per-session queues | single-writer SQLite | SQLCipher-encrypted, read-only inbound | ✅ **stronger** |
| Channels | broad | Slack · Discord · Telegram · Teams · Signal · iMessage · Webhook · WhatsApp · Email · Matrix · Google Chat · web chat | ✅ **at parity** (was a gap; closed) |
| Outbound + interactive tools | yes | send / file / ask / schedule / tasks / a2a `create_agent` | ✅ at parity |
| Scheduling & multi-agent (a2a) | yes | yes (RFC-0004, gateway-gated `create_agent`) | ✅ at parity |
| Published threat model | partial / none | full STRIDE + data-flow + privilege matrix | ✅ **ahead** |
| Checked-in OpenAPI / API contract | varies | versioned `api/openapi.yaml` | ✅ ahead |
| Web UI / dashboard | community / **full Control UI** | embedded mesh-only console | ✅ at parity |
| Skills / plugin registry | yes (ClawHub) | host-side, signed, gateway-gated capability bundles | ✅ shipped |
| MCP / external tool servers | yes (blind approval surface) | host-brokered, isolated, **per-tool human-approved** + audited | ✅ **ahead** |
| Guided onboarding | wizard | `ironctl onboard` + quickstart | ✅ at parity |
| Credential vault (arbitrary APIs) | yes | logical-name `vault://` injection via a separate host-side injector behind the gateway-approved egress broker (per-group deny-by-default, agent never holds a key) | ✅ **shipped** (1.0) |
| Multiple LLM providers | drop-in modules | Anthropic / OpenAI / OpenRouter / Codex via host proxy | ✅ at parity |
| In-product diagnostics | `/status` `/usage` | Prometheus metrics + audit + `ironctl status`/`doctor` | ✅ at parity |
| **Signed releases + SBOM + provenance** | neither peer | cosign-signed releases + SBOM + build provenance, reproducible `ironctl`/`sandbox` | ✅ **shipped** (a win neither peer has claimed) |

**The short version:** IronClaw is *ahead on the security spine* — provable
isolation, a mandatory approval gateway, encrypted queues, and a published threat
model — and has *closed most of the product-surface gap* (channels, an embedded web
console, skills, and multi-provider support are all shipped). The supply-chain trust
items — cosign-signed releases, an SBOM, and build provenance — have now shipped too;
they're differentiators neither peer has claimed, and reproducible builds are landing
component by component.

---

*This page is the single source of truth for the roadmap. For the architecture and
security design, see [`architecture.md`](architecture.md) and
[`threat-model.md`](threat-model.md); for the engineering build-log of the security
backend (Waves 0–5), see the
[README roadmap](https://github.com/IronSecCo/ironclaw#roadmap).*
