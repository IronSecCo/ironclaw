# Task Graph — IronClaw (MECE, first-come-first-serve)

**Claim rule:** any idle agent claims the lowest-wave, highest-priority task whose `status: available`,
whose `depends_on` are all `done`, and whose locks are free. No fixed assignment. Safety = **disjoint
`owned_paths` within a wave** + waves + locks. See [`../AGENTS.md`](../AGENTS.md). Authoritative list:
[`task-registry.json`](task-registry.json).

---

## ✅ Wave 1 — COMPLETE (skeleton → wired + core features)

T-001..T-004 bootstrap · T-010 queue factory · T-011 sandbox entrypoint · T-012 rootfs spike ·
T-013a/b/c/d + T-030 parity · T-014 doc drift · T-015 durable registry · T-016 daemon wiring ·
T-020 SessionManager · T-021 sweep adapters · T-022 rootfs provisioning · T-040 Telegram adapter ·
T-080 admin CRUD API · T-081 ironctl subcommands · T-082 send_message/send_file ·
T-083 ask_user_question (RFC-0003) · T-084 task-management tools · T-085 durable memory/workspace.
**Still open from Wave 1:** T-086 a2a + create_agent (RFC-0004 PROPOSED, `agent:needs-human`, issue #20).

---

## Wave 4 — Production hardening (parallel-safe; disjoint path scopes)

| Task | Scope | Gap | Pri |
|---|---|---|---|
| T-100 durable/pluggable master-key custody | `internal/host/keys/**` | G-019 | P0 |
| T-101 structured logging (slog) | `internal/obs/**` (new) | G-020 | P1 |
| T-102 Prometheus metrics package | `internal/host/metrics/**` (new) | G-021 | P1 |
| T-103 API hardening (TLS, rate-limit, body limits, /readyz, /metrics) | `internal/host/api/**` | G-022 | P1 |
| T-104 OCI resource limits + seccomp | `internal/host/isolation/oci.go`,`seccomp.go` | G-023 | P1 |
| T-105 host respawn crash-loop backoff | `internal/host/sweep/**` | G-024 | P1 |
| T-106 sandbox provider backoff + circuit breaker | `internal/sandbox/loop/**`,`provider/**` | G-025 | P1 |
| T-107 model proxy rate caps + audit + redaction | `internal/host/modelproxy/**` | G-026 | P1 |
| T-108 end-to-end lifecycle test | `test/e2e/**` (new) | G-027 | P1 |
| T-109a Slack adapter · T-109b Discord adapter | `internal/host/channels/{slack,discord}.go` | G-028 | P2 |
| T-113 deployment units + installer + sandbox image | `deploy/**`,`container/**` (`lock:release`) | G-032 | P2 |
| T-114 session-state persistence across restart | `internal/sandbox/queue/**` | G-033 | P2 |
| T-116 roadmap + docs refresh | `README.md`,`docs/threat-model.md`,`docs/architecture.md` | G-034 | P3 |
| T-118 image signature/digest verification | `internal/host/isolation/provisioner.go` | G-035 | P2 |

## Wave 5 — Integration + future-work (some human-gated)

| Task | Scope | Gap | Status |
|---|---|---|---|
| T-120 daemon wiring v2 (composes new subsystems) | `cmd/controlplane/**` | several | blocked ← T-100,T-102,T-103,T-105,T-107 |
| T-110 Kata isolation backend | `internal/host/isolation/kata.go` | G-029 | available (P3) |
| T-111 egress broker for approved external APIs | `internal/host/egress/**` (new) | G-030 | needs-human |
| T-112 gateway auto-approval policy + RBAC | `internal/host/gateway/**` | G-031 | needs-human |
| T-086 a2a + create_agent (RFC-0004) | `delivery` + sandbox tool + **contract** | G-018 | needs-human (`lock:contract`) |

## Dependency DAG (deps → blocks)

```
T-100 ┐
T-102 ┤
T-103 ┼→ T-120
T-105 ┤
T-107 ┘
```
All other Wave-4 tasks are independent and immediately claimable.

## Collision audit (file-level within the shared packages)

- `internal/host/isolation/**` is split by file: T-104 → `oci.go`+`seccomp.go`; T-118 → `provisioner.go`;
  T-110 → `kata.go`. Disjoint files; soft-coordinate on the `Isolator` interface.
- `internal/host/channels/**`: T-109a → `slack.go`, T-109b → `discord.go` (disjoint). `channels.go`
  (the interface) is **not** owned by either; adapter registration in `cmd/controlplane/main.go` is wired
  by T-120 (single-owner shared file).
- `cmd/controlplane/main.go` → only T-120.
- `internal/sandbox/queue/**` (T-114) is disjoint from `loop/**`+`provider/**` (T-106).
- `internal/contract/**` → only T-086, **human-gated** (`lock:contract`).
- `docs/contract.md` → no task (RFC log; T-116 explicitly excludes it).

## Coverage matrix (Wave 4/5 gaps)

G-019→T-100 · G-020→T-101 · G-021→T-102 · G-022→T-103 · G-023→T-104 · G-024→T-105 · G-025→T-106 ·
G-026→T-107 · G-027→T-108 · G-028→T-109a/b · G-029→T-110 · G-030→T-111 · G-031→T-112 · G-032→T-113 ·
G-033→T-114 · G-034→T-116 · G-035→T-118. (G-018→T-086 carried from Wave 1.)

---

## ✅ Waves 4–5 — COMPLETE

All of T-086, T-100…T-120 landed (every GitHub issue closed). The **security backend is done**:
durable keys, structured logging, metrics, API hardening, OCI limits + seccomp, respawn/provider
backoff, model-proxy caps, e2e test, Slack + Discord adapters, deployment units + installer + sandbox
image, session-state persistence, image signature verification, daemon wiring v2, Kata backend, egress
broker, gateway policy/RBAC, and a2a + create_agent (RFC-0004). See `task-registry.json` `completed`.

---

# Road to 1.0 — Waves 6–8 (product parity + OSS launch + web UI)

The backend is complete; these waves close the gap to a **public, community-grade product**. Same FCFS
rule, same disjoint-`owned_paths` safety. New tasks: T-200…T-260 (see `task-registry.json`).

## Wave 6 — Public-launch readiness (do first; mostly XS–M, several `good first issue`)

| Task | Scope | Gap | Pri |
|---|---|---|---|
| T-200 SECURITY.md + Private Vulnerability Reporting | `SECURITY.md` | G-044 | P0 |
| T-201 CODE_OF_CONDUCT.md (Contributor Covenant) | `CODE_OF_CONDUCT.md` | G-044 | P0 |
| T-202 issue templates (forms) + PR template | `.github/ISSUE_TEMPLATE/**`,`.github/pull_request_template.md` | G-044 | P0 |
| T-203 README overhaul (hero, asciinema, badges) | `README.md` | G-045 | P0 |
| T-204 repo metadata (description, topics, social preview) | `docs/assets/**` + repo settings | G-045 | P0 |
| T-205 docker-compose + .env.example + GHCR image | `docker-compose.yml`,`.env.example`,`.github/workflows/image.yml` (`lock:ci`,`lock:release`) | G-046 | P0 |
| T-206 guided onboarding wizard (`ironctl onboard`) | `cmd/ironctl/onboard.go`,`internal/host/onboard/**` | G-038 | P1 |
| T-207 5-minute quickstart | `docs/quickstart.md` | G-038 | P1 |
| T-208 Homebrew tap + CHANGELOG | `CHANGELOG.md`,`packaging/homebrew/**` (`lock:release`) | G-052 | P1 |
| T-209 public-repo ruleset (keeps push-to-main) | `.github/rulesets/main.json` (`lock:ci`) | G-058 | P0 |
| T-210 Discussions + Discord + good-first-issues | repo settings + `.github/DISCUSSIONS.md` | G-053 | P1 |

## Wave 7 — Product parity + web UI

| Task | Scope | Gap | Pri |
|---|---|---|---|
| **T-220 [spike] web console architecture + scaffold** | `web/**`,`.agents/spikes/web-console.md` | G-037 | P1 |
| T-221 Web UI: approvals inbox | `web/src/routes/approvals/**`,`internal/host/api/ui_approvals.go` | G-037 | P1 ← T-220 |
| T-222 Web UI: sessions browser | `web/src/routes/sessions/**`,`…/ui_sessions.go` | G-037 | P2 ← T-220 |
| T-223 Web UI: channels & wiring | `web/src/routes/channels/**`,`…/ui_channels.go` | G-037 | P2 ← T-220 |
| T-224 Web UI: logs & audit viewer | `web/src/routes/logs/**`,`…/ui_audit.go` | G-037 | P2 ← T-220 |
| T-225 Web UI: config editor + web wizard | `web/src/routes/setup/**`,`…/ui_config.go` | G-037/G-038 | P2 ← T-220 |
| T-226 Web UI: chat playground | `web/src/routes/chat/**`,`…/ui_chat.go` | G-037 | P2 ← T-220 |
| **T-227 [spike] host-side skills system** | `.agents/spikes/skills-system.md` | G-036 | P2 · needs-human |
| T-228 channel: WhatsApp | `internal/host/channels/whatsapp.go` | G-039 | P2 |
| T-229 channel: Email/Gmail | `internal/host/channels/email.go` | G-039 | P2 |
| T-230 channel: Matrix | `internal/host/channels/matrix.go` | G-039 | P3 |
| T-231 channel: Google Chat | `internal/host/channels/googlechat.go` | G-039 | P3 |
| T-232 channels: Teams/iMessage/Signal (tracking) | `internal/host/channels/{teams,imessage,signal}.go` | G-039 | P3 |
| T-233 multi-provider model support | `internal/host/modelproxy/**`,`internal/sandbox/provider/**` | G-040 | P3 · needs-human |
| T-234 first-class persona/identity | `internal/host/registry/persona.go`,`internal/sandbox/tools/persona.go` | G-042 | P2 |
| T-235 observability CLI (status/doctor/usage) | `cmd/ironctl/{status,doctor}.go` | G-043 | P2 |

## Wave 8 — Trust, supply-chain & ecosystem

| Task | Scope | Gap | Pri |
|---|---|---|---|
| T-250 docs site (Mintlify/Fumadocs) | `docs/site/**` | G-047 | P1 |
| T-251 OpenAPI spec checked in + rendered | `api/openapi.yaml` | G-048 | P1 |
| T-252 threat-model expansion (STRIDE + DFD) | `docs/threat-model.md` | G-049 | P1 |
| T-253 signed releases + SBOM + provenance | `.goreleaser.yaml`,`.github/workflows/release.yml` (`lock:release`) | G-050 | P1 |
| T-254 supply-chain hygiene (Dependabot/CodeQL/secret-scan) | `.github/dependabot.yml`,`.github/workflows/codeql.yml` (`lock:ci`) | G-051 | P1 |
| T-255 OpenSSF Scorecard + Best Practices badges | `.github/workflows/scorecard.yml` (`lock:ci`) | G-054 | P2 |
| T-256 SLSA L3 + reproducible builds | `.github/workflows/slsa.yml`,`docs/reproducible-builds.md` (`lock:release`) | G-054 | P2 |
| T-257 examples gallery + templates | `examples/**` | G-055 | P2 |
| T-258 public roadmap + demo media + comparison | `docs/roadmap.md`,`docs/assets/demo/**` | G-056 | P2 |
| T-259 third-party security audit | `docs/audits/**` | G-057 | P2 · needs-human |
| T-260 end-user credential vault | `internal/host/egress/vault.go` | G-041 | P3 · needs-human |

## Dependency DAG (Road to 1.0)

```
T-220 (web scaffold) ──→ T-221 … T-226   (all UI features depend on the scaffold)
Wave 6 is otherwise flat & independent (each file/area disjoint) — claim in any order.
Wave 8 release-workflow tasks serialize on lock:release (T-205, T-208, T-253, T-256);
       CI-workflow tasks serialize on lock:ci (T-205, T-209, T-254, T-255).
needs-human spikes (T-227, T-233, T-259, T-260) wait for a human decision before code.
```

## Collision audit (Road to 1.0)

- `internal/host/channels/**` — one disjoint `<platform>.go` per task (T-228..T-232), the T-109 pattern.
  `channels.go` (interface) is owned by none; adapter registration in `cmd/controlplane/main.go` is the
  daemon-wiring owner's job — **adapter tasks must not edit `main.go`**.
- `web/**` — T-220 owns the scaffold; T-221..T-226 each own a disjoint `web/src/routes/<feature>/**`
  subtree + one `internal/host/api/ui_<feature>.go` read-model file. Pairwise disjoint.
- `.github/workflows/**` is a soft-lock: T-205/T-208/T-209/T-253/T-254/T-255/T-256 coordinate via
  `lock:ci` / `lock:release` (serialize), never edit the same workflow file concurrently.
- `internal/contract/**` — touched by **no** Road-to-1.0 task. Any that needs it → STOP + joint RFC.

## Coverage matrix (Road to 1.0)

G-036→T-227 · G-037→T-220..T-226 · G-038→T-206/T-207/T-225 · G-039→T-228..T-232 · G-040→T-233 ·
G-041→T-260 · G-042→T-234 · G-043→T-235 · G-044→T-200/T-201/T-202 · G-045→T-203/T-204 · G-046→T-205 ·
G-047→T-250 · G-048→T-251 · G-049→T-252 · G-050→T-253 · G-051→T-254 · G-052→T-208 · G-053→T-210 ·
G-054→T-255/T-256 · G-055→T-257 · G-056→T-258 · G-057→T-259 · G-058→T-209. Every gap mapped.
