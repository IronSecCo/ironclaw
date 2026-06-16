# IronClaw — Project Review + Master Planner Artifacts

## Context

You asked for two things: (1) an engineering health review of IronClaw, and (2) execution of the
`docs/MASTER_PLANNER_AGENT_PROMPT.md` role — producing the MECE task system that lets a **pool of
interchangeable worker agents** start safely. You chose to also create **GitHub Issues** and to
**promote `AGENTS.md` to the repo root**.

**Worker model (this is the key design constraint):** there is **no fixed assignment** of work to a
specific agent. Any number of agents run concurrently; whoever is idle claims the next eligible task
**first-come-first-serve**. Tasks are therefore made safe by their **path scope, wave, and locks** —
not by who runs them. Two agents never collide because no two concurrently-claimable tasks own
overlapping paths, not because they were pre-assigned to different people.

IronClaw is a pre-alpha, security-first Go platform: a host **control-plane** and a per-session
**gVisor sandbox** that talk only through encrypted per-session SQLite queues, with a mandatory
human-approval gateway on every mutation. A **frozen contract** (`internal/contract/**`) is the only
package both sides import; it is RFC-gated and must not be edited without a joint RFC.

The review found the architecture sound and the unit-level pieces real (198 tests pass, CI green),
but the **daemon does not yet wire the components into a live session lifecycle**, and several docs
and code comments have **drifted out of sync with the applied RFCs**. This plan captures that as a
gap analysis and a wave-based task graph, then emits the planner artifacts and GitHub issues.

This is a planning deliverable: **no product source code is modified**. The only code-adjacent change
is promoting the governance doc `AGENTS.md` to the root. All implementation tasks are authored for
*worker* agents to claim later, not executed here.

---

## Approved scope (this turn): FULL rollout — repo files + GitHub Issues

The user approved the full rollout so the work is accessible to everyone and two remote machines can claim tasks:
1. `git pull --ff-only origin main` (16dc12e → 33bb237).
2. Generate repo files: root `AGENTS.md` (adapted per Part 5), `.agents/{repo-map,gap-analysis,task-graph}.md` + `task-registry.json`, `.agents/PLAN.md` (copy of this plan), and `scripts/agent/{preflight,push}.sh` (CAS infra, T-004).
3. Keep the two untracked `docs/*.md` and commit everything.
4. `git push origin main`.
5. GitHub: create the unified labels, one `[T-0xx]` Issue per task (unassigned, claimable), and the pinned **Agent Coordination Board** issue; post the Master Planner Report there.
6. Branch protection on `main`: block force-push + deletion only (direct-push CAS mode — do not restrict pushers).

The machine-readable, claimable task breakdown becomes `.agents/task-registry.json` (in-repo) + the `[T-0xx]` GitHub Issues. Done tasks T-013d and T-030 are recorded as such; T-040/T-086 carry `agent:needs-human`.

---

## Part 1 — Health Assessment (advisory)

**Strong:**
- Clean, security-driven architecture. A **frozen contract** plus **path-scoped ownership** keeps concurrent work from colliding and prevents silent runtime drift (`CONTRIBUTING.md`, `CODEOWNERS`).
- RFC-0001 (encrypted SQLite binding) and RFC-0002 (cross-seam wire formats, `internal/contract/actions.go`) are applied and tested at the contract level.
- 198 tests pass across ~30 files; `make build vet test` green with `CGO_ENABLED=1`.
- Real security posture: no script field in scheduling (no RCE class), `network=none`, gVisor spec, read-only inbound enforced at type + mount level, host-only credential injection.

**Gaps / risks (evidence):**
- **Daemon session lifecycle is not wired.** `cmd/controlplane/main.go` leaves the key custodian unused (`_ = custodian`, line 64) and the isolator unused (`_ = isolator`, line 129); the sweep hooks are log-only placeholders (lines 200–243). Router, delivery, per-session queues, and isolation are unit-tested but never composed into the running daemon.
- **Doc/comment drift.** `docs/architecture.md` "What remains gated" still says `OpenInboundRW` doesn't exist and openers "return the pending-binding error" (lines 86–98); `cmd/controlplane/main.go` and `cmd/sandbox/main.go` comments still say the loop won't start "until the encrypted SQLite binding is wired in." RFC-0001 has landed — these are stale.
- **Sandbox entrypoint** (`cmd/sandbox/main.go`) reflects the same stale assumption and likely exits early instead of starting the loop against the live binding.
- **`test/parity/` specs are empty stubs** (11–24 lines each: routing, delivery, engage, gateway, session, crossmount, sandbox_outbound) — the black-box behavioral suite is unwritten.
- **Registry is in-memory only**; durable encrypted backend designed but not built.
- **Rootfs provisioning** is the one documented external integration point (`isolation.ErrRootfsMissing`, `isolation.go:179–215`).
- **Autonomous protocol not live**: no root `AGENTS.md`, no `.agents/` registry, no agent labels/coordination board (only default GitHub labels exist).

---

## Part 2 — Gap Analysis (drives the task graph)

| Gap | Category | Path scope | Evidence |
|---|---|---|---|
| G-001 Autonomous protocol not live (no AGENTS.md / .agents / labels / board) | infra | `AGENTS.md`, `.agents/**`, GitHub | root has no `AGENTS.md`, no `.agents/`; `gh label list` = defaults only |
| G-002 Per-session encrypted queue factory not wired into daemon | bug | `internal/host/queue/**` | `host/queue` openers exist post-RFC-0001 but daemon never creates per-session DBs |
| G-003 Router + delivery not composed into a session manager | bug | `internal/host/session/**` (new) | `RouteInbound`/`Poll` unit-tested only; not driven by the daemon |
| G-004 Isolation + key custodian not wired (no handle tracking) | bug | `internal/host/session/**` (new) | `main.go:64,129` unused; no live launch/kill path |
| G-005 Sweep hooks are log-only placeholders | bug | `internal/host/sweep/**` | `main.go:200–243` healthyProber/logKiller/emptyDueSource/logWaker/logEnqueue |
| G-006 Daemon entrypoint doesn't compose the live lifecycle | bug | `cmd/controlplane/**` | `cmd/controlplane/main.go` |
| G-007 Sandbox entrypoint stale; doesn't start loop on live binding | bug | `cmd/sandbox/**` | `cmd/sandbox/main.go` header + early-exit comment |
| G-008 Rootfs provisioning (external image unpacker) | infra/spike | `internal/host/isolation/**`, `deploy/**` | `isolation.go:179–215`, `ErrRootfsMissing` |
| G-009 Black-box parity specs unwritten | test | `test/parity/<file>` | `test/parity/*_test.go` are 11–24-line stubs |
| G-010 Durable encrypted registry backend missing | security | `internal/host/registry/**` | `registry` in-memory only (`architecture.md:32–47`) |
| G-011 Doc/comment drift vs applied RFC-0001 | docs | `docs/**` | `architecture.md:86–98`; stale comments in both entrypoints |
| G-012 Concrete channel adapters beyond webhook reference | feature | `internal/host/channels/**` | only `WebhookAdapter` exists; platform choice is a product decision |
| G-013 No runtime provisioning surface (agent/messaging groups, wirings, users, roles, members, destinations) | feature | `internal/host/api/**`, `cmd/ironctl/**` | API is gateway-only (`/v1/changes`,`/v1/audit`); registry filled only by `seedDev` |
| G-014 Sandbox lacks outbound messaging tools (`send_message`, `send_file`) | feature | `internal/sandbox/tools/**` | tools are only `schedule_task`/`request_capability_change`/`read_file`/`write_file`/`list_dir` |
| G-015 No interactive `ask_user_question` / choice-card tool | feature | `internal/sandbox/tools/**`, `internal/host/registry/**` | nanoclaw has `ask_user_question`; IronClaw has none |
| G-016 Task management verbs beyond `schedule_task` | feature | `internal/sandbox/tools/**`, `internal/host/scheduling/**` | only `schedule_task`; no list/cancel/pause/resume/update |
| G-017 No durable per-group memory / persistent workspace / shared mount | feature | `internal/host/isolation/**` | `/workspace` is ephemeral tmpfs; no CLAUDE.md-style group memory or global RO mount |
| G-018 No agent-to-agent messaging or dynamic `create_agent` | feature | (architecture decision) | nanoclaw supports multi-agent send + approval-gated `create_agent` |

---

## Part 3 — MECE Task Graph (first-come-first-serve)

Every task carries a **single path scope** (`owned_paths`) and a set of **forbidden_paths**. The rule
that keeps the agent pool safe: **no two tasks that are simultaneously claimable own overlapping
paths.** Any idle agent may claim any task whose status is `available`, whose `depends_on` are all
`done`, and whose required locks are free — lowest wave first, highest priority first. There is no
per-agent assignment; the registry, not a human, decides eligibility.

Three serialization mechanisms replace fixed ownership:
- **Waves** gate ordering (a task is only claimable once its wave opens / its deps are `done`).
- **`lock:contract`** — any task that would touch `internal/contract/**` is blocked and must stop and
  file a joint RFC (`docs/contract.md`). No task in this plan touches it.
- **Single-owner shared files** — the one shared file, `cmd/controlplane/main.go`, is owned by exactly
  one task (T-016), which sits in the final wave behind the packages it composes.

**Wave 0 — Bootstrap (the planner does these on approval):**
- T-001 **Adapt + promote** the protocol → root `AGENTS.md` (not a verbatim copy — see Part 5). owns: `AGENTS.md`.
- T-002 Emit `.agents/{repo-map,gap-analysis,task-graph}.md` + `task-registry.json`. owns: `.agents/**`.
- T-003 Create agent labels + per-task Issues + Agent Coordination Board issue. owns: GitHub state.
- T-004 Stand up **direct-push CAS** infrastructure: `scripts/agent/preflight.sh` (CGO: `make build vet test`) + `scripts/agent/push.sh` (fetch → rebase `origin/main` → preflight → non-force push → retry ≤3×); ensure CI runs **on `main`** as the broken-main guard; set branch protection to **block force-push + block deletion only** (do NOT restrict who pushes — every agent must be able to push under CAS). owns: `.github/**`, `scripts/agent/**`. No integrator bot/workflow, no queue refs.

**Wave 1 — Independent foundation (all parallel-safe; disjoint path scopes):**
- T-010 Per-session encrypted queue factory. owns: `internal/host/queue/**`. → G-002
- T-011 Wire sandbox entrypoint to the live binding; start the loop; fix stale header. owns: `cmd/sandbox/**`. → G-007
- T-012 (spike) Research rootfs / image-unpacker approach (containerd vs OCI tool). owns: `.agents/spikes/rootfs.md`. → G-008
- T-013a Parity specs: `routing_test.go` + `engage_test.go`. owns: those two files. → G-009
- T-013b Parity specs: `delivery_test.go` + `gateway_test.go`. owns: those two files. → G-009
- T-013c Parity spec: `session_test.go`. owns: that file. → G-009
- ~~T-013d Parity spec: `sandbox_outbound_test.go`~~ — **DONE** (landed in `33bb237`: `TestSandboxOutboundSeqParity` + `TestSandboxAcksProcessing`).
- T-014 Fix RFC-0001 doc drift. owns: `docs/architecture.md`, `docs/building.md`. → G-011
- T-015 Durable encrypted registry backend behind the `Registry` interface. owns: `internal/host/registry/**`. → G-010

**Wave 2 — Dependent implementation:**
- T-020 New `internal/host/session/**` SessionManager composing queues + router + delivery + isolation + keys. owns: the new package only. depends_on: T-010. → G-003, G-004
- T-021 Real sweep adapters (Prober/Killer/DueSource/Waker/Enqueue) bound to live deps. owns: `internal/host/sweep/**`. depends_on: T-010. → G-005
- T-022 Rootfs provisioning implementation. owns: `internal/host/isolation/**`, `deploy/**`. depends_on: T-012. → G-008

**Wave 3 — Integration / hardening:**
- T-016 Daemon wiring: compose SessionManager + real sweep into `cmd/controlplane/main.go`; remove the `_ =` placeholders; fix stale comments. owns: `cmd/controlplane/**`. depends_on: T-020, T-021. → G-006
- ~~T-030 `crossmount_test.go` live encrypted cross-mount parity~~ — **DONE** (landed in `33bb237`: `TestCrossMountLivePoll`, validated at the encrypted-queue level — no rootfs/daemon dependency). A full gVisor end-to-end remains implicit under T-022 (rootfs) but is not a separate parity task.
- T-040 Concrete channel adapter(s) — platform selection is a product decision. owns: `internal/host/channels/**`. label `agent:needs-human`. → G-012

**Wave additions — from the nanoclaw-v2 capability diff (Part 6):**
- T-080 Registry admin CRUD API: create/list/update/delete agent groups, messaging groups, wirings, users, roles, members, destinations over the existing `Registry` methods. owns: `internal/host/api/**`. wave 2. P1. → G-013
- T-081 `ironctl` resource subcommands wrapping T-080 (groups/wirings/users/roles/members/destinations). owns: `cmd/ironctl/**`. depends_on: T-080. wave 3. → G-013
- T-082 Sandbox outbound messaging tools: `send_message` (named destination / current thread) + `send_file`, enforced by host destination permissions (already in `delivery`). owns: `internal/sandbox/tools/messaging.go`, `internal/sandbox/tools/destinations.go`. wave 2. → G-014
- T-083 Sandbox interactive tool: `ask_user_question` (choice card) + host pending-question tracking. owns: `internal/sandbox/tools/interactive.go` + `internal/host/registry/**` pending-questions (coordinate with T-015). wave 3. → G-015
- T-084 Task-management tools: `list_tasks`/`cancel_task`/`pause_task`/`resume_task`/`update_task` over the existing scheduling store. owns: `internal/sandbox/tools/tasks.go` + `internal/host/scheduling/**`. wave 3. → G-016
- T-085 Durable per-group memory + persistent workspace + read-only global shared mount (today `/workspace` is ephemeral tmpfs). owns: `internal/host/isolation/**`, `deploy/**`. depends_on: T-022 (serialize on `isolation/**`). wave 3. P2. → G-017
- T-086 Multi-agent: agent-to-agent messaging + approval-gated `create_agent` (new gateway change-kind). label `agent:needs-human` (architecture decision). wave 3. → G-018

**Note — sandbox tool registration is a shared seam.** T-082/083/084 each own distinct *files*, but all register into the loop's tool set (`internal/sandbox/loop`). That registration point is a **soft-lock**: the tasks coordinate (or serialize) on the one registration site; their tool implementations stay in disjoint files.

**Collision audit:** `cmd/controlplane/main.go` → only T-016. Each parity stub file → exactly one task.
The new `internal/host/session/**` package is created solely by T-020. `internal/contract/**` is touched
by **no** task (any agent that finds it must, per lock, stop and file an RFC). Within each wave all
`owned_paths` are pairwise disjoint, so any subset of agents can claim in any order without conflict.

**Coverage:** G-001→T-001/002/003/004 · G-002→T-010 · G-003→T-020 · G-004→T-020 · G-005→T-021 ·
G-006→T-016 · G-007→T-011 · G-008→T-012/T-022 · G-009→T-013a/b/c (T-013d + T-030 **done** in `33bb237`) ·
G-010→T-015 · G-011→T-014 · G-012→T-040 · G-013→T-080/T-081 · G-014→T-082 · G-015→T-083 ·
G-016→T-084 · G-017→T-085 · G-018→T-086. Every gap mapped; T-040/T-086 flagged `agent:needs-human`.

---

## Part 5 — AGENTS.md adjustments (before agents run)

`docs/AGENTS_MAIN_ONLY_AUTONOMOUS_PROTOCOL.md` is sound but generic. Before promoting it to root
`AGENTS.md`, T-001 applies these IronClaw-specific edits so worker agents act on real commands and
real invariants:

1. **Frozen-contract lock (new, highest priority).** Add `lock:contract` covering `internal/contract/**`.
   Rule: a task that needs a contract change must **stop and escalate as `agent:needs-human`** with a
   joint RFC appended to `docs/contract.md` — it is **never** auto-landed through the queue. This
   reconciles the no-PR model with `CODEOWNERS`/`CONTRIBUTING.md` (both owners must approve the contract).
2. **Real high-risk path list.** Replace the JS/Python hard-lock list (§5.2) with IronClaw's:
   `internal/contract/**` (→ `lock:contract`, human), `go.mod`/`go.sum` (→ `lock:dependency`),
   `.github/**` (→ `lock:ci`), `Makefile`, `deploy/**` (→ `lock:release`), `AGENTS.md`,
   `.agents/task-registry.json`, and the shared entrypoint `cmd/controlplane/main.go`.
3. **Go/CGO preflight.** Rewrite the preflight (§10) and the integrator workflow checks (§9) to the
   real toolchain: `CGO_ENABLED=1 gofmt -l .` (format), `CGO_ENABLED=1 go vet ./...` (lint/typecheck),
   `CGO_ENABLED=1 go build ./...` (build), `CGO_ENABLED=1 go test ./...` (tests). Drop the
   `scripts/install.sh`/`format-check.sh`/etc. placeholders; `scripts/agent/preflight.sh` becomes a
   thin wrapper over `make build vet test` with `CGO_ENABLED=1`.
4. **Unified labels.** Make AGENTS.md and the issues agree on one set: lifecycle `agent:ready /
   claimed / in-progress / blocked / needs-human / done / failed / reverted`, plus the planner's
   `wave:0..3`, `size:XS/S/M/L/spike`, `priority:P0..P3`, `category:*`, and `lock:*` (incl. new
   `lock:contract`). Standardize on `agent:needs-human` (fix the planner prompt's `needs:human`).
5. **Mark dead-weight sections N/A for this stack.** DB migrations (§14), schema/proto/GraphQL (§15),
   generated files (§16), and per-agent test databases (§18.2) don't exist in IronClaw — annotate them
   "not applicable to the current Go stack; revisit if introduced" so agents don't invent work.
6. **FCFS wording.** Soften §23 "orchestrator assigns work" → the planner *publishes* `agent:ready`
   tasks and agents **self-select first-come-first-serve** (lowest wave, highest priority, deps `done`,
   locks free). Make the no-fixed-assignment rule explicit at the top of the Operating Model (§0).
7. **Dependency examples in Go.** §13 examples become `go get <mod>@<ver>` + `go mod tidy` under
   `lock:dependency`, not `npm install`.
8. **Re-anchor to direct-push CAS mode (per your choice).** Promote §11 to the **canonical landing
   path** and demote the queue + integrator machinery (§8, §9, §22, §5.3 global main-writer lock,
   §2.2 bot identity) to an "optional alternative — not used in this repo." Revise §0 and §1.2 so the
   rule reads: *every agent may push to `main` directly, non-force, only through the
   fetch → rebase → CGO-preflight → push → retry-≤3× loop; a rejected push means `main` moved — rebase,
   retest, retry.* Because direct push raises broken-main risk, keep §24 (Broken-Main Protocol)
   prominent and make **CI-on-main green** a hard precondition before the next agent pushes.

T-001 keeps the protocol's safety spine unchanged: main-only truth, **no force-push, no reset**,
revert-first on broken main, scope discipline, lease/heartbeat claiming, and structured
issue-comment logs. Only the *landing mechanism* changes (CAS instead of queue+integrator).

---

## Part 6 — nanoclaw-v2 capability diff

Compared IronClaw (current + after all planned tasks) against `../nanoclaw-v2` (a TypeScript/Docker
personal-assistant platform with the same host↔per-session-container + dual-SQLite shape). IronClaw
matches or exceeds nanoclaw on the **security spine** (gVisor + `network=none` vs Docker; deterministic
human-approval gateway vs role checks; encrypted per-session queues; sealed Go binary). The gaps are on
**product surface**:

| nanoclaw-v2 capability | IronClaw status | Verdict |
|---|---|---|
| Channel adapters (Slack/Telegram/Discord/WhatsApp/…) | only `WebhookAdapter` | gap → G-012/T-040 |
| Reasoning loop + provider | present (`sandbox/loop`+`provider`, Anthropic via host proxy) | ✅ (multi-provider optional, not essential) |
| Outbound tools (`send_message`,`send_file`) | missing | **gap → G-014/T-082** |
| Interactive `ask_user_question` | missing | **gap → G-015/T-083** |
| Task mgmt (list/cancel/pause/resume/update) | only `schedule_task` | **gap → G-016/T-084** |
| Scheduling/recurrence | present (`scheduling`+`schedule_task`) | ✅ |
| Permissions + approvals | present, stronger (mandatory gateway) | ✅ |
| Container isolation | present, stronger (gVisor) | ✅ |
| Admin CLI / resource CRUD (`ncl`) | `ironctl` is gateway-only | **gap → G-013/T-080+T-081** |
| Multi-agent groups + routing | present (registry+router, session modes) | ✅ |
| Agent-to-agent + `create_agent` | missing | gap → G-018/T-086 (needs-human) |
| Per-group/global memory + workspace | ephemeral tmpfs only | **gap → G-017/T-085** |
| Observability/logging | present (audit JSONL + heartbeat) | ✅ |

**Deliberately excluded — by IronClaw's threat model (do NOT build these; flagged so agents don't "fill" them):**
- **Web access / browser automation** (nanoclaw `agent-browser`) — the sandbox is `network=none`; only model egress via the host proxy. Would require an egress broker (documented future "egress firewalling"); `agent:needs-human` if ever pursued.
- **`install_packages` / self-modification** — the "sealed runtime" pillar forbids in-sandbox mutation by design.
- **General credential vault** (nanoclaw OneCLI `HTTPS_PROXY` for arbitrary APIs) — IronClaw injects only the model credential host-side; arbitrary-API egress is out of scope by threat model.
- **Multiple LLM providers** (OpenCode/Ollama) — optional, low priority; IronClaw is Anthropic-via-proxy.

New tasks T-080…T-086 (placed in the wave plan above) onboard the essential, in-threat-model gaps.
Excluded items are recorded so worker agents treat them as intentional non-goals, not unfinished work.

---

## Part 4 — Execution (on approval)

0. **Sync to latest `main`**: `git pull --ff-only origin main` to fast-forward the working tree from `16dc12e` to `33bb237` (already fetched; brings in the completed parity specs) before generating artifacts.
1. **Adapt + promote governance doc**: write root `AGENTS.md` from `docs/AGENTS_MAIN_ONLY_AUTONOMOUS_PROTOCOL.md` with the Part 5 edits applied (frozen-contract lock, real high-risk paths, Go/CGO preflight, unified labels, N/A annotations, FCFS wording, direct-push CAS as the canonical landing path). Leave the original doc in `docs/` for provenance.
2. **CAS infra (T-004)**: add `scripts/agent/preflight.sh` (`CGO_ENABLED=1 make build vet test`) and `scripts/agent/push.sh` (fetch → rebase → preflight → non-force push → retry ≤3×); confirm CI runs on `main`; set branch protection to block force-push + deletion only.
3. **Create `.agents/` artifacts** per `MASTER_PLANNER_AGENT_PROMPT.md` §10:
   - `repo-map.md` (base SHA **`33bb237`** — rebased onto the latest `origin/main` fetched in Step 1, Go 1.23 + CGO, dir map, `make build vet test`, CI summary, high-risk shared files: `internal/contract/**`, `cmd/controlplane/main.go`, `go.mod`).
   - `gap-analysis.md` (Part 2 table + evidence + risk).
   - `task-graph.md` (Part 3 waves, DAG, parallel-safe groups, coverage matrix, FCFS claim rule).
   - `task-registry.json` (valid JSON; each task in the §8 schema: `status: available`, `owned_paths`, `forbidden_paths`, `locks_required`, `depends_on`/`blocks`, `acceptance_criteria`, `validation_commands` = `CGO_ENABLED=1 go test ./...`, `estimated_conflict_risk`). No `assignee` field — claiming is dynamic.
4. **GitHub**: create the unified labels (lifecycle `agent:ready/claimed/in-progress/blocked/needs-human/done/failed/reverted`, `wave:0..3`, `size:XS/S/M/L/spike`, `priority:P0..P3`, `category:*`, `lock:*` incl. `lock:contract`); open one Issue per task `[T-0xx] …` with the §9 body (owned/forbidden paths, deps, acceptance criteria, CGO validation commands, non-goals) — issues are **unassigned**, claimed by whoever comments `/agent-claim` first; open the **Agent Coordination Board** issue listing waves and ready-to-claim tasks.
5. **Post the Master Planner Report** (§14 structure) to the Coordination Board, ending with the worker rule: *claim any `agent:ready` task with no unmet deps, lowest wave + highest priority first.*

Boundaries honored: no edits to `internal/**` or `cmd/**` source; contract untouched; `go.mod`/lockfile untouched.

---

## Verification

- `git status` shows only new `AGENTS.md`, `.agents/**`, `scripts/agent/**`, and the existing untracked docs — no edits to `internal/**` or `cmd/**`.
- `AGENTS.md` reflects the Part 5 edits: contains `lock:contract` → `internal/contract/**`, the Go/CGO preflight, the unified label set, and direct-push CAS as the canonical landing path (queue/integrator marked optional).
- `bash -n scripts/agent/preflight.sh && bash -n scripts/agent/push.sh` parse; preflight uses `CGO_ENABLED=1`.
- `python3 -m json.tool .agents/task-registry.json` parses (valid JSON); no task has an `assignee`.
- Coverage check: every G-id appears in `task-graph.md`'s matrix and every T-id maps to ≥1 gap.
- Disjointness check: within each wave, no two tasks share an `owned_paths` entry.
- `gh label list` shows the unified agent labels; `gh issue list` shows unassigned `[T-0xx]` issues + the Coordination Board.
- Branch protection on `main`: force-push and deletion blocked; pushes NOT restricted to one identity.
- `CGO_ENABLED=1 make build vet test` still green (unchanged — proves planning artifacts didn't touch the build).
