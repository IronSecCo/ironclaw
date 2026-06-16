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

In-sandbox web/browser access; `install_packages`/self-modification; general credential vault for
arbitrary APIs; multiple LLM providers. These are intentional non-goals of the sealed / `network=none`
design — recorded so agents don't treat them as unfinished work.
