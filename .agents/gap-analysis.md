# Gap Analysis — IronClaw

Each gap maps to ≥1 task in [`task-registry.json`](task-registry.json) / [`task-graph.md`](task-graph.md).
Evidence is file:line at base `33bb237`.

| Gap | Category | Risk | Path scope | Evidence |
|---|---|---|---|---|
| G-001 Autonomous protocol not live | infra | low | `AGENTS.md`, `.agents/**`, GitHub | resolved by this rollout (T-001..T-004) |
| G-002 Per-session encrypted queue factory not wired into daemon | bug | med | `internal/host/queue/**` | openers exist post-RFC-0001; daemon never creates per-session DBs |
| G-003 Router+delivery not composed into a session manager | bug | high | `internal/host/session/**` (new) | `RouteInbound`/`Poll` unit-tested only |
| G-004 Isolation + key custodian not wired | bug | high | `internal/host/session/**` (new) | `cmd/controlplane/main.go:64,129` unused |
| G-005 Sweep hooks are log-only placeholders | bug | med | `internal/host/sweep/**` | `cmd/controlplane/main.go:200-243` |
| G-006 Daemon entrypoint doesn't compose live lifecycle | bug | high | `cmd/controlplane/**` | `cmd/controlplane/main.go` |
| G-007 Sandbox entrypoint stale; loop not started on live binding | bug | med | `cmd/sandbox/**` | `cmd/sandbox/main.go` header/early-exit comment |
| G-008 Rootfs provisioning (external image unpacker) | infra/spike | med | `internal/host/isolation/**`, `deploy/**` | `isolation.go:179-215`, `ErrRootfsMissing` |
| G-009 Black-box parity specs unwritten | test | low | `test/parity/<file>` | routing/engage/delivery/gateway/session still stubs (outbound+crossmount done in `33bb237`) |
| G-010 Durable encrypted registry backend missing | security | med | `internal/host/registry/**` | registry in-memory only (`architecture.md`) |
| G-011 Doc/comment drift vs applied RFC-0001 | docs | low | `docs/**` | `architecture.md:86-98` says binding still pending |
| G-012 Concrete channel adapters beyond webhook | feature | med | `internal/host/channels/**` | only `WebhookAdapter` (needs-human: platform choice) |
| G-013 No runtime provisioning surface | feature | high | `internal/host/api/**`, `cmd/ironctl/**` | API is gateway-only; registry filled only by `seedDev` |
| G-014 Sandbox lacks outbound messaging tools | feature | med | `internal/sandbox/tools/**` | tools only schedule/capability/read/write/list |
| G-015 No interactive `ask_user_question` tool | feature | low | `internal/sandbox/tools/**`, `internal/host/registry/**` | absent |
| G-016 Task-management verbs beyond `schedule_task` | feature | low | `internal/sandbox/tools/**`, `internal/host/scheduling/**` | only `schedule_task` |
| G-017 No durable per-group memory / persistent workspace | feature | med | `internal/host/isolation/**` | `/workspace` ephemeral tmpfs |
| G-018 No agent-to-agent messaging / `create_agent` | feature | med | (architecture decision) | absent (needs-human) |

## Wave 4/5 gaps — production hardening + future work (added after Wave 1 completed)

Evidence at base `02748dd` (post-Wave-1).

| Gap | Category | Risk | Path scope | Evidence |
|---|---|---|---|---|
| G-019 Master key ephemeral (no durable/KMS custody) | security | high | `internal/host/keys/**` | `cmd/controlplane/main.go:64` generates master with crypto/rand each boot; session keys not durable |
| G-020 Unstructured logging (no slog/JSON) | infra | med | `internal/obs/**` | plain `log.Printf` throughout; no trace IDs |
| G-021 No metrics (Prometheus /metrics) | infra | med | `internal/host/metrics/**` | no counters/histograms anywhere |
| G-022 API not hardened (no TLS/rate-limit/body-limit/readyz) | security | high | `internal/host/api/**` | bearer token only; bare `http.Server` (api.go) |
| G-023 No OCI resource limits + custom seccomp | security | high | `internal/host/isolation/oci.go` | no mem/cpu/pids caps; OCI defaults only |
| G-024 No host respawn crash-loop backoff | bug | med | `internal/host/sweep/**` | sweep respawns immediately; no failure counter |
| G-025 No sandbox provider backoff/circuit-breaker | bug | med | `internal/sandbox/loop/**` | fixed 2s retry forever (loop.go:146) |
| G-026 Model proxy lacks rate caps/audit/redaction | security | med | `internal/host/modelproxy/**` | modelproxy.go:12 flags as future work |
| G-027 No end-to-end lifecycle test | test | med | `test/e2e/**` | only unit + queue-level parity; smoke_test is a no-op |
| G-028 Few channel adapters (webhook + Telegram only) | feature | med | `internal/host/channels/**` | no Slack/Discord/WhatsApp |
| G-029 No Kata isolation backend | feature | low | `internal/host/isolation/kata.go` | only runsc; Isolator interface exists |
| G-030 No egress firewalling/broker for external APIs | security | low | `internal/host/egress/**` | threat-model documents as future work |
| G-031 No gateway auto-approval policy / RBAC | security | low | `internal/host/gateway/**` | AlwaysRequireHuman floor only |
| G-032 Deployment scaffold-only (no real units/installer/image) | infra | med | `deploy/**`,`container/**` | install.sh is a comment scaffold |
| G-033 No session-state persistence across restart | feature | med | `internal/sandbox/queue/**` | loop buffers are in-memory only |
| G-034 README roadmap + docs drift | docs | low | `README.md`,`docs/**` | roadmap lists landed items as unchecked |
| G-035 No sandbox image signature verification | security | med | `internal/host/isolation/provisioner.go` | provisioner pulls image as-is |

## Human decisions required

- **G-012 / T-040** — which chat platforms to support first (Slack/Telegram/Discord/…).
- **G-018 / T-086** — agent-to-agent + `create_agent` model (new gateway change-kind).
- Any task that needs a `internal/contract/**` change → **stop, joint RFC, `agent:needs-human`.**

## Out of scope by threat model (do NOT build)

**Permanent non-goals** (intentional, by the sealed / `network=none` design — agents must not treat these as unfinished work):
- In-sandbox web/browser access and **in-sandbox `install_packages` / self-modification**. The seal (no in-sandbox mutation) is non-negotiable.

**Reconsidered as human-gated** (no longer "never" — the egress broker T-111 landed, relaxing the absolute `network=none`-only posture *under gateway control*): general egress to approved APIs (**done**, T-111), an **end-user credential vault** (now `agent:needs-human` T-260), and **multiple LLM providers** (now `agent:needs-human` T-233). These touch the network/threat posture, so they stay human-decision tasks, not free-for-all builds.

---

## Wave 6–8 gaps — product parity + OSS launch readiness (Road to 1.0)

**Context:** Waves 0–5 (T-001…T-120) are **all landed** — the security backend is complete (host control-plane, gVisor/Kata isolation, encrypted queues, gateway, Slack/Discord/Telegram/Webhook channels, scheduling, registry, metrics, durable keys, model proxy, egress broker, a2a/create_agent, daemon wiring v2). The remaining gaps are **not backend** — they are product surface (vs nanoclaw/openclaw), an absent **web UI**, **onboarding** polish, and **open-source launch readiness**. Evidence: current codebase inventory + the nanoclaw/openclaw feature comparison + GitHub community-standards review (June 2026).

### Product-parity gaps (vs `nanocoai/nanoclaw` + `openclaw/openclaw`)

| Gap | Category | Risk | Path scope | Evidence |
|---|---|---|---|---|
| G-036 No skills/extension system | feature | med | (architecture decision) | peers' headline extensibility (SKILL.md + ClawHub 13.7k skills; nanoclaw `/add-*`); IronClaw forbids in-sandbox install — needs a host-side gateway-approved mechanism (needs-human) |
| G-037 No web UI / dashboard / console | feature | high | `web/**` (new), `internal/host/api/**` | CLI + API only; openclaw ships a 6-tab Control UI; biggest structural gap |
| G-038 No guided onboarding wizard | feature | med | `cmd/ironctl/**`, `internal/host/onboard/**` (new) | setup is manual env wiring vs `nanoclaw.sh` / `openclaw onboard` (minutes to first message) |
| G-039 Limited channel breadth | feature | med | `internal/host/channels/**` | have Slack/Discord/Telegram/Webhook; missing WhatsApp/email/Matrix/Google Chat/Teams/iMessage/Signal (peers: 13–23+) |
| G-040 Single model provider | feature | low | `internal/host/modelproxy/**` | Anthropic-via-proxy only; peers allow per-agent provider (needs-human; touches egress posture) |
| G-041 No end-user credential vault | security | low | `internal/host/egress/**` | peers use OneCLI request-time injection; IronClaw injects only the model cred (needs-human; builds on T-111) |
| G-042 No first-class persona/identity surface | feature | low | `internal/host/registry/**`, `internal/sandbox/tools/**` | peers' SOUL.md/CLAUDE.md + default name; IronClaw has a persona change-kind but no identity surface |
| G-043 No in-product observability/self-diagnostics | feature | low | `cmd/ironctl/**` | peers ship `/status` `/trace` `/usage` + `doctor`; IronClaw has metrics but no operator CLI |

### OSS launch-readiness gaps (GitHub community standards + category bar)

| Gap | Category | Risk | Path scope | Evidence |
|---|---|---|---|---|
| G-044 Missing community-health files | docs | high | `SECURITY.md`,`CODE_OF_CONDUCT.md`,`.github/**` | only CONTRIBUTING.md + LICENSE exist; no SECURITY policy (critical for a security product), CoC, or issue/PR templates |
| G-045 README not launch-grade; no repo metadata | docs | med | `README.md`, repo settings | no hero/asciinema/social-preview; no topics/description tuned for discovery |
| G-046 No docker-compose + .env.example + image | infra | high | `docker-compose.yml`,`.env.example` | the category's standard front door (`docker compose up -d`) is absent |
| G-047 No docs site | docs | med | `docs/site/**` (new) | README-only reads pre-1.0; both peers run docs.* sites |
| G-048 No published OpenAPI spec | docs | med | `api/**` | for a UI-less product the spec *is* the contract; none checked in |
| G-049 Threat-model doc thin vs category bar | security | med | `docs/threat-model.md` | needs STRIDE-per-boundary + data-flow; security is the battlefield IronClaw should win |
| G-050 Releases unsigned; no SBOM/provenance | security | high | `.goreleaser.yaml`,`.github/**` | a security tool shipping unsigned binaries undercuts its thesis; neither peer signs — a win available |
| G-051 Supply-chain hygiene off | security | high | `.github/**` | no Dependabot/CodeQL/secret-scanning/SHA-pinned actions |
| G-052 No Homebrew tap; no CHANGELOG | infra | low | `packaging/**`,`CHANGELOG.md` | Go binary is a distribution edge; standard CLI install + changelog missing |
| G-053 No support community / good-first-issues | docs | med | repo settings | no Discussions/Discord; empty tracker reads as inactive |
| G-054 No trust badges; no SLSA/reproducible builds | security | low | `.github/**` | OpenSSF Scorecard/Best-Practices + SLSA L3 differentiate vs peers |
| G-055 No examples gallery / templates | docs | low | `examples/**` | every exemplar ships a gallery + a headline count |
| G-056 No public roadmap / demo media | docs | low | `docs/**` | park "Web UI" on a public roadmap so the gap reads as planned |
| G-057 No third-party security audit | security | low | `docs/audits/**` | strongest trust signal; neither peer has one — a differentiator |
| G-058 No public-repo branch ruleset | infra | med | repo settings | default branch unprotected; must keep push-to-main yet add integrity |

### Coverage (new gaps → tasks)

G-036→T-227 · G-037→T-220..T-226 · G-038→T-206/T-207/T-225 · G-039→T-228..T-232 · G-040→T-233 ·
G-041→T-260 · G-042→T-234 · G-043→T-235 · G-044→T-200/T-201/T-202 · G-045→T-203/T-204 · G-046→T-205 ·
G-047→T-250 · G-048→T-251 · G-049→T-252 · G-050→T-253 · G-051→T-254 · G-052→T-208 · G-053→T-210 ·
G-054→T-255/T-256 · G-055→T-257 · G-056→T-258 · G-057→T-259 · G-058→T-209.

### Human decisions required (Road to 1.0)

- **G-036 / T-227** — add a host-side, gateway-approved skills/extension system despite the no-in-sandbox-install seal? (trust model for third-party skills)
- **G-040 / T-233** — relax Anthropic-only to per-agent providers? (egress + audit posture)
- **G-041 / T-260** — build vs integrate an end-user credential vault on top of the egress broker?
- **G-057 / T-259** — engage a third-party security audit (scope/budget)?
