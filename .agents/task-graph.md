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

## Coverage matrix (new gaps)

G-019→T-100 · G-020→T-101 · G-021→T-102 · G-022→T-103 · G-023→T-104 · G-024→T-105 · G-025→T-106 ·
G-026→T-107 · G-027→T-108 · G-028→T-109a/b · G-029→T-110 · G-030→T-111 · G-031→T-112 · G-032→T-113 ·
G-033→T-114 · G-034→T-116 · G-035→T-118. (G-018→T-086 carried from Wave 1.)
