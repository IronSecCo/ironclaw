# IronClaw — public roadmap (Road to 1.0)

A living view of where IronClaw is and where it's going. The **security backend
(Waves 0–5) is complete**; this page tracks the road to a 1.0 *product* —
public-launch readiness, product parity (including a web UI), and best-in-class
supply-chain trust. It is regenerated against the GitHub issue tracker, so a row
here maps to a real `[T-…]` issue.

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
| **Wave 8** | Trust, supply-chain & ecosystem | 🚧 threat model + OpenAPI + examples done |

The backend is done; the remaining work is product surface, a UI, onboarding,
and open-source/supply-chain polish — **not** the security core.

## Wave 6 — Public-launch readiness

Everything needed to flip the repo public credibly.

| Task | What | Status |
|------|------|--------|
| [T-200](https://github.com/nivardsec/ironclaw/issues/41) | `SECURITY.md` + private vulnerability reporting | ✅ |
| [T-201](https://github.com/nivardsec/ironclaw/issues/42) | `CODE_OF_CONDUCT.md` | 🚧 |
| [T-202](https://github.com/nivardsec/ironclaw/issues/43) | Issue forms + PR template | ✅ |
| [T-203](https://github.com/nivardsec/ironclaw/issues/44) | Launch-grade README (hero, demo, badges) | ✅ |
| [T-204](https://github.com/nivardsec/ironclaw/issues/45) | Repo description, topics, social-preview image | ⬜ |
| [T-205](https://github.com/nivardsec/ironclaw/issues/46) | `docker-compose.yml` + `.env.example` + published image | ⬜ |
| [T-206](https://github.com/nivardsec/ironclaw/issues/47) | Guided `ironctl onboard` wizard | ✅ |
| [T-207](https://github.com/nivardsec/ironclaw/issues/48) | 5-minute quickstart tutorial | ✅ |
| [T-208](https://github.com/nivardsec/ironclaw/issues/49) | Homebrew tap + CHANGELOG + release notes | ✅ |
| [T-209](https://github.com/nivardsec/ironclaw/issues/50) | Public-repo ruleset for push-to-main | ✅ |
| [T-210](https://github.com/nivardsec/ironclaw/issues/51) | Discussions + Discord + seeded good-first-issues | ⬜ |

## Wave 7 — Product parity & web UI

The product surface that brings IronClaw level with the category, on its stronger
security base.

### Web UI (the biggest structural gap — planned, not missing)

CLI- and API-first is a deliberate feature today (no public web surface to
attack). A private, **loopback-only** web console — reusing the API token, never
widening the network posture — is planned for Wave 7, gated on an architecture
spike so the stack isn't guessed:

| Task | What | Status |
|------|------|--------|
| [T-220](https://github.com/nivardsec/ironclaw/issues/52) | **Spike** — web console architecture + scaffold | ⬜ |
| [T-221](https://github.com/nivardsec/ironclaw/issues/53) | Approvals inbox (the gateway in a browser — the killer first feature) | ⬜ |
| [T-222](https://github.com/nivardsec/ironclaw/issues/54) | Sessions browser | ⬜ |
| [T-223](https://github.com/nivardsec/ironclaw/issues/55) | Channels & wiring management | ⬜ |
| [T-224](https://github.com/nivardsec/ironclaw/issues/56) | Logs & audit viewer | ⬜ |
| [T-225](https://github.com/nivardsec/ironclaw/issues/57) | Config editor + web onboarding wizard | ⬜ |
| [T-226](https://github.com/nivardsec/ironclaw/issues/58) | Chat playground | ⬜ |

### Channels, persona & observability

| Task | What | Status |
|------|------|--------|
| [T-228](https://github.com/nivardsec/ironclaw/issues/60) | Channel adapter: WhatsApp | ✅ |
| [T-229](https://github.com/nivardsec/ironclaw/issues/61) | Channel adapter: Email / Gmail | ✅ |
| [T-230](https://github.com/nivardsec/ironclaw/issues/62) | Channel adapter: Matrix | ✅ |
| [T-231](https://github.com/nivardsec/ironclaw/issues/63) | Channel adapter: Google Chat | ✅ |
| [T-232](https://github.com/nivardsec/ironclaw/issues/64) | Teams / iMessage / Signal (tracking) | ⬜ |
| [T-234](https://github.com/nivardsec/ironclaw/issues/66) | First-class persona / identity surface | ⬜ |
| [T-235](https://github.com/nivardsec/ironclaw/issues/67) | Observability CLI (`ironctl status` / `doctor` / usage) | ⬜ |
| [T-227](https://github.com/nivardsec/ironclaw/issues/59) | Host-side skills / extension system (spike) | 👤 |
| [T-233](https://github.com/nivardsec/ironclaw/issues/65) | Multi-provider model support | 👤 |

With the four adapters above shipped, IronClaw now speaks Slack, Discord,
Telegram, Webhook, WhatsApp, Email/SMTP, Matrix, and Google Chat.

## Wave 8 — Trust, supply-chain & ecosystem

Press the security advantage — several of these are wins neither peer has claimed.

| Task | What | Status |
|------|------|--------|
| [T-250](https://github.com/nivardsec/ironclaw/issues/68) | Documentation site | ⬜ |
| [T-251](https://github.com/nivardsec/ironclaw/issues/69) | Checked-in OpenAPI spec | ✅ |
| [T-252](https://github.com/nivardsec/ironclaw/issues/70) | Threat model — STRIDE + data-flow | ✅ |
| [T-253](https://github.com/nivardsec/ironclaw/issues/71) | Signed releases + SBOM + provenance | ⬜ |
| [T-254](https://github.com/nivardsec/ironclaw/issues/72) | Supply-chain hygiene (Dependabot / CodeQL / secret scanning / pinned actions) | ⬜ |
| [T-255](https://github.com/nivardsec/ironclaw/issues/73) | OpenSSF Scorecard + Best-Practices badges | ⬜ |
| [T-256](https://github.com/nivardsec/ironclaw/issues/74) | SLSA L3 provenance + reproducible builds | ⬜ |
| [T-257](https://github.com/nivardsec/ironclaw/issues/75) | Examples gallery + templates | ✅ |
| [T-258](https://github.com/nivardsec/ironclaw/issues/76) | Public roadmap + demo media + comparison (this page) | ✅ |
| [T-259](https://github.com/nivardsec/ironclaw/issues/77) | Third-party security audit | 👤 |
| [T-260](https://github.com/nivardsec/ironclaw/issues/78) | End-user credential vault | 👤 |

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
| Channels | broad | Slack · Discord · Telegram · Webhook · WhatsApp · Email · Matrix · Google Chat | ✅ **at parity** (was a gap; closed) |
| Outbound + interactive tools | yes | send / file / ask / schedule / tasks / a2a `create_agent` | ✅ at parity |
| Scheduling & multi-agent (a2a) | yes | yes (RFC-0004, gateway-gated `create_agent`) | ✅ at parity |
| Published threat model | partial / none | full STRIDE + data-flow + privilege matrix | ✅ **ahead** |
| Checked-in OpenAPI / API contract | varies | versioned `api/openapi.yaml` | ✅ ahead |
| **Web UI / dashboard** | community / **full Control UI** | CLI + API today | ⬜ **planned** (Wave 7) — biggest gap |
| **Skills / plugin registry** | yes (ClawHub) | none | 👤 planned (needs a design decision) |
| Guided onboarding | wizard | `ironctl onboard` + quickstart | ✅ at parity |
| Credential vault (arbitrary APIs) | yes | model credential + gateway-approved egress broker | 👤 partial (vault planned) |
| Multiple LLM providers | drop-in modules | Anthropic via host proxy | 👤 planned |
| In-product diagnostics | `/status` `/usage` | Prometheus metrics + audit (operator CLI planned) | 🚧 partial |
| **Signed releases + SBOM + reproducible builds** | neither peer | planned (Wave 8) | 🎯 **win available** |

**The short version:** IronClaw is *ahead on the security spine* — provable
isolation, a mandatory approval gateway, encrypted queues, and a published threat
model — and is *closing the product-surface gap* (channels are done; the web UI,
skills, and multi-provider support are on this roadmap). The supply-chain trust
items (signing, SBOM, reproducible builds) are differentiators neither peer has
claimed yet, and they're next.

---

*This roadmap reflects the plan in the project's internal Road-to-1.0 analysis and
the live GitHub issue tracker. For the architecture and security design, see
[`architecture.md`](architecture.md) and [`threat-model.md`](threat-model.md).*
