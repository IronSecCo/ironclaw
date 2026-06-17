# IronClaw — Road to 1.0

*Fresh evaluation, June 2026. Base `cd9911c`. Supersedes the stale `PLAN.md` Part 6 nanoclaw diff.*

This is the **full work plan to 100%**: a fresh feature comparison against the `claw` ecosystem
(`nanocoai/nanoclaw`, `openclaw/openclaw`), an open-source launch-readiness review against GitHub
community standards + best-in-class self-hostable peers, and the wave-based task plan that closes both.
Every item below is a real GitHub issue (`[T-2xx]`) and a row in [`task-registry.json`](task-registry.json).

---

## 1. Where we are — the backend is done

**Waves 0–5 (T-001…T-120) are all landed; every GitHub issue is closed.** IronClaw today is a complete,
security-first agent backend:

- **Isolation:** per-session gVisor (runsc) sandbox, `network=none`, all caps dropped, read-only rootfs,
  pluggable rootfs provisioning + image signature verification; Kata backend behind the same interface.
- **Seam:** encrypted per-session SQLite queues (SQLCipher, RFC-0001), a **frozen contract**, durable keys.
- **Control-plane:** deterministic gateway with a mandatory human-approval floor (+ opt-in policy/RBAC),
  registry (groups/wirings/users/roles/destinations/sessions), router, delivery, scheduling, sweep with
  respawn backoff, model proxy with rate caps + audit + redaction, **gateway-approved egress broker**,
  Prometheus metrics, structured logging, hardened API (TLS/rate-limit/readyz).
- **Sandbox tools:** `send_message`, `send_file`, `read_file`, `write_file`, `list_dir`,
  `ask_user_question`, `schedule_task`, `list_tasks`, `cancel_task`, `pause_task`, `resume_task`,
  `update_task`, `list_destinations`, `http_fetch`, `request_capability_change`, `create_agent` (a2a, RFC-0004).
- **Channels:** Slack, Discord, Telegram, Webhook. **CLI:** `ironctl` (gateway + full registry CRUD).
- **Ops:** multi-platform release pipeline (signed-checksum binaries), `install.sh`/`install.ps1`,
  systemd/launchd units, sandbox container image, end-to-end lifecycle test.

**On the security spine IronClaw already matches or beats both peers** (OS-isolation like nanoclaw, but
gVisor + `network=none` + a deterministic approval gateway vs nanoclaw's Docker and openclaw's host-access
default). The gap to a 1.0 *product* is **not backend** — it is product surface, a UI, onboarding, and
open-source polish.

---

## 2. Fresh comparison vs the `claw` ecosystem

Primary reference **`nanocoai/nanoclaw`** (TS, "lightweight alternative to OpenClaw that runs in containers
for security"); category bar **`openclaw/openclaw`** (ships a full Control UI + Canvas + native apps).
Ecosystem: per-channel adapter repos, `nanoclaw-skills`, **ClawHub** registry (13.7k+ skills),
**OneCLI** credential vault, **clawsec** security suite, microclaw.

| Capability | nanoclaw / openclaw | IronClaw | Verdict |
|---|---|---|---|
| Container isolation | Docker / opt-in | gVisor + `network=none` + Kata | ✅ **stronger** |
| Approval / permissions | role checks / host access | mandatory deterministic gateway | ✅ **stronger** |
| Encrypted per-session queues | single-writer SQLite | SQLCipher encrypted | ✅ **stronger** |
| Channels | 13 / 23+ | Slack·Discord·Telegram·Webhook | ⚠️ **gap → G-039** (WhatsApp/email/Matrix/GChat/Teams/iMessage/Signal) |
| Outbound + interactive tools | yes | yes (send/file/ask/schedule/tasks/create_agent) | ✅ |
| Scheduling / proactivity | yes (sweep + recurrence) | yes | ✅ |
| Multi-agent + a2a | yes | yes (RFC-0004) | ✅ |
| Memory / workspace | `CLAUDE.md` per group | durable per-group workspace | ✅ (persona surface → G-042) |
| **Skills / plugin system + registry** | **yes (ClawHub, `/add-*`)** | **none** | ❌ **gap → G-036** (needs-human) |
| **Web UI / dashboard** | community / **full Control UI** | **none (CLI+API)** | ❌ **biggest gap → G-037** |
| **Guided onboarding wizard** | `nanoclaw.sh` / `openclaw onboard` | manual env wiring | ❌ **gap → G-038** |
| Credential vault (arbitrary APIs) | OneCLI Agent Vault | model cred only (+ egress broker) | ⚠️ **gap → G-041** (needs-human) |
| Multiple LLM providers | drop-in modules | Anthropic-via-proxy | ⚠️ **gap → G-040** (needs-human) |
| In-product observability / `doctor` | `/status` `/trace` `/usage` | metrics, no operator CLI | ⚠️ **gap → G-043** |
| Signed releases + SBOM + reproducible | **neither peer** | not yet | 🎯 **win available → G-050/G-054** |
| Published threat model + audit | nanoclaw doc; no audits | thin doc; none | 🎯 **win available → G-049/G-057** |

**Read:** IronClaw is *behind on product surface* (UI, skills, channel breadth, onboarding) and *ahead on
security* — and several security-credibility items (signing/SBOM/threat-model/audit) are **wins neither peer
has claimed yet**. The plan leans into both: close the product gaps, then press the security advantage.

---

## 3. Open-source launch readiness

IronClaw is going **private → public**. Today only `LICENSE` + `CONTRIBUTING.md` exist. Measured against
GitHub's community-standards checklist and best-in-class self-hostable peers (n8n, Dify, LibreChat,
OpenHands, Home Assistant, Supabase, Coolify), the launch gaps are:

- **Community-health files (P0):** `SECURITY.md` + Private Vulnerability Reporting (critical for a security
  product — *both peers fail GitHub's check here*), `CODE_OF_CONDUCT.md`, issue/PR templates. → G-044
- **First impression (P0):** a launch-grade README (hero command, asciinema cast in place of the
  screenshot a UI-less product lacks, ≤4 badges), repo description/topics/social-preview. → G-045
- **The standard front door (P0):** `docker compose up -d` + `.env.example` + a published image. → G-046
- **Onboarding (P1):** a guided `ironctl onboard` wizard + a 5-minute quickstart. → G-038
- **Docs + API (P1):** a docs site (Mintlify/Fumadocs) and a checked-in OpenAPI spec. → G-047/G-048
- **Supply-chain trust (P1, the differentiator):** signed releases + SBOM + provenance (GoReleaser/cosign/
  syft), Dependabot/CodeQL/secret-scanning, an expanded STRIDE threat model. → G-050/G-051/G-049
- **Distribution + community (P1):** Homebrew tap + CHANGELOG, Discussions + Discord + seeded
  good-first-issues. → G-052/G-053
- **Maturity (P2):** OpenSSF Scorecard + Best-Practices badges, SLSA L3 + reproducible builds, an examples
  gallery, a public roadmap + demo media, and eventually a third-party audit. → G-054/G-055/G-056/G-057

**The three things that most differentiate IronClaw from its peers — all winnable on day one:** a complete,
correctly-located community-health file set; signed releases + SBOM + reproducible builds; and a published
threat model plus an eventual third-party audit.

---

## 4. The UI (called out explicitly)

The single biggest structural gap. openclaw's **Control UI** is the bar: a zero-install SPA served on a
loopback port with Chat / Sessions / Config / Nodes / Logs / Skills tabs. IronClaw is CLI + API only.

Plan (Wave 7, gated on a spike so we don't guess the stack): **T-220** decides the architecture
(recommended: an embedded Go-served SPA bound loopback-only, reusing the API token — the UI must never widen
the network posture) and lands the scaffold; then **T-221 Approvals inbox** (IronClaw's killer first
feature — the human-approval gateway in a browser), **T-222 Sessions**, **T-223 Channels/wiring**,
**T-224 Logs/audit**, **T-225 Config editor + web onboarding wizard**, **T-226 Chat playground**. Until it
ships, we **frame CLI+API-first as a feature** (cf. OpenHands) and park "Web UI" on the public roadmap so the
gap reads as *planned*, not *missing*.

---

## 5. The plan — Waves 6–8 (T-200…T-260)

Same FCFS model as the backend waves: any idle agent claims the lowest-wave, highest-priority `agent:ready`
task whose deps are done and locks are free; safety = **disjoint `owned_paths`** + waves + locks. Full
breakdown, DAG, and collision audit in [`task-graph.md`](task-graph.md); authoritative specs in
[`task-registry.json`](task-registry.json).

- **Wave 6 — Public-launch readiness (11 tasks, mostly XS–M, several `good first issue`).** Everything
  needed to flip the repo public credibly: health files, README, docker-compose, onboarding wizard,
  quickstart, Homebrew, ruleset, community. **This is the critical path to "public."**
- **Wave 7 — Product parity + web UI (16 tasks).** The web console (spike + 6 features), 5+ new channels,
  persona surface, observability CLI, and two needs-human spikes (skills system, multi-provider).
- **Wave 8 — Trust, supply-chain & ecosystem (11 tasks).** Docs site, OpenAPI, expanded threat model,
  signed releases + SBOM, supply-chain hygiene, Scorecard/SLSA, examples, public roadmap, audit, vault.

### What "100%" means
- **Public-ready (end of Wave 6):** the repo can go public and meet every GitHub community standard + the
  category's onboarding bar.
- **At parity (end of Wave 7):** a user gets nanoclaw/openclaw's product experience — a UI, broad channels,
  guided setup — on IronClaw's stronger security base.
- **Best-in-class trust (end of Wave 8):** signed/reproducible builds, SBOM, a published threat model, and
  an audit — the security high ground neither peer holds.

### Human decisions to unblock (4)
`T-227` skills system · `T-233` multi-provider · `T-260` credential vault · `T-259` third-party audit.
Each is filed `agent:needs-human` per our convention — a maintainer decides scope before any code.

---

*Generated alongside the GitHub issues by [`scripts/agent/seed_roadmap.py`](../scripts/agent/seed_roadmap.py)
— the single source of truth for the Road-to-1.0 backlog (registry + issues).*
