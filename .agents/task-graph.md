# Task Graph — IronClaw (MECE, first-come-first-serve)

**Claim rule:** any idle agent claims the lowest-wave, highest-priority task whose `status: available`,
whose `depends_on` are all `done`, and whose locks are free. No fixed assignment. Safety comes from
**disjoint `owned_paths` within a wave** + waves + locks — see [`../AGENTS.md`](../AGENTS.md).

Authoritative machine-readable list: [`task-registry.json`](task-registry.json).

## Waves

**Wave 0 — Bootstrap (done by this rollout):** T-001 AGENTS.md · T-002 `.agents/**` · T-003 issues+labels+board · T-004 CAS infra + branch protection.

**Wave 1 — Independent foundation (parallel-safe, disjoint scopes):**
- T-010 per-session encrypted queue factory — `internal/host/queue/**`
- T-011 sandbox entrypoint → live binding + start loop — `cmd/sandbox/**`
- T-012 (spike) rootfs/image-unpacker research — `.agents/spikes/rootfs.md`
- T-013a parity routing+engage · T-013b parity delivery+gateway · T-013c parity session — `test/parity/<file>`
- T-014 RFC-0001 doc-drift fix — `docs/architecture.md`,`docs/building.md`
- T-015 durable encrypted registry backend — `internal/host/registry/**`

**Wave 2 — Dependent implementation:**
- T-020 SessionManager (queues+router+delivery+isolation+keys) — `internal/host/session/**` (new); deps T-010
- T-021 real sweep adapters — `internal/host/sweep/**`; deps T-010
- T-022 rootfs provisioning impl — `internal/host/isolation/**`,`deploy/**`; deps T-012
- T-080 registry admin CRUD API — `internal/host/api/**` (P1)
- T-082 sandbox `send_message`/`send_file` — `internal/sandbox/tools/messaging.go`,`destinations.go`

**Wave 3 — Integration / hardening:**
- T-016 daemon wiring — `cmd/controlplane/**`; deps T-020,T-021
- T-040 concrete channel adapter(s) — `internal/host/channels/**` (needs-human)
- T-081 ironctl resource subcommands — `cmd/ironctl/**`; deps T-080
- T-083 `ask_user_question` — `internal/sandbox/tools/interactive.go` (+registry pending-questions)
- T-084 task-management verbs — `internal/sandbox/tools/tasks.go` (+scheduling)
- T-085 durable per-group memory / persistent workspace — `internal/host/isolation/**`,`deploy/**`; deps T-022 (P2)
- T-086 agent-to-agent + `create_agent` — needs-human

**Already done (landed in `33bb237`):** T-013d (`sandbox_outbound_test.go`), T-030 (`crossmount_test.go`).

## Dependency DAG (deps → blocks)

```
T-010 → T-020, T-021
T-012 → T-022 → T-085
T-020 → T-016
T-021 → T-016
T-080 → T-081
```
No cycles. Wave 1 + T-080/T-082 are immediately claimable.

## Collision audit

- `cmd/controlplane/main.go` → only T-016.
- Each parity stub file → exactly one task.
- New `internal/host/session/**` → only T-020.
- `internal/host/isolation/**` → T-022 then T-085 (serialized by dep).
- `internal/host/registry/**` → T-015; T-083 adds pending-questions there → coordinate/serialize (soft-lock).
- Sandbox tool files are disjoint (`messaging.go`/`interactive.go`/`tasks.go`); registration in
  `internal/sandbox/loop` is a **soft-lock** the three coordinate on.
- `internal/contract/**` → **no task** (human-gated `lock:contract`).

## Coverage matrix

G-001→T-001/002/003/004 · G-002→T-010 · G-003→T-020 · G-004→T-020 · G-005→T-021 · G-006→T-016 ·
G-007→T-011 · G-008→T-012/T-022 · G-009→T-013a/b/c (T-013d,T-030 done) · G-010→T-015 · G-011→T-014 ·
G-012→T-040 · G-013→T-080/T-081 · G-014→T-082 · G-015→T-083 · G-016→T-084 · G-017→T-085 · G-018→T-086.
