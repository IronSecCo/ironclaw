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

## Human decisions required

- **G-012 / T-040** — which chat platforms to support first (Slack/Telegram/Discord/…).
- **G-018 / T-086** — agent-to-agent + `create_agent` model (new gateway change-kind).
- Any task that needs a `internal/contract/**` change → **stop, joint RFC, `agent:needs-human`.**

## Out of scope by threat model (do NOT build)

In-sandbox web/browser access; `install_packages`/self-modification; general credential vault for
arbitrary APIs; multiple LLM providers. These are intentional non-goals of the sealed / `network=none`
design — recorded so agents don't treat them as unfinished work.
