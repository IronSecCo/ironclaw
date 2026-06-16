# AGENTS.md — IronClaw Autonomous Main-Only Multi-Agent Protocol

> Every agent must read this file before touching code. Human maintainer instructions override it.
>
> This is the IronClaw-tailored profile of the general protocol in
> [`docs/AGENTS_MAIN_ONLY_AUTONOMOUS_PROTOCOL.md`](docs/AGENTS_MAIN_ONLY_AUTONOMOUS_PROTOCOL.md).
> Where this file and that document differ, **this file wins** for IronClaw.

---

## 0. Operating model — first-come-first-serve, main-only

- A pool of interchangeable agents runs concurrently on different machines. **There is no fixed
  assignment.** Whoever is idle claims the next eligible task; tasks are made safe by their **path
  scope, wave, and locks**, not by who runs them.
- Accepted changes land **directly on `main`** (no pull requests for normal work).
- **Landing mechanism: direct-push CAS.** Each agent rebases on `origin/main`, runs the CGO preflight,
  and pushes `main` non-force through `scripts/agent/push.sh`. A rejected push means `main` moved —
  rebase, retest, retry (≤3×). The queue + integrator machinery in the general protocol (§8, §9, §22)
  is an **optional alternative and is not used in this repo.**
- **Never force-push or reset `main`.** If `main` breaks, the fix is a revert commit (§7 below).

## 1. Hard safety invariants

1. `origin/main` is the only shared truth. Run `git fetch origin main --prune` before start, before
   test, and before push. Assume your local view is stale otherwise.
2. Every agent may push `main` directly, **non-force**, **only** via the
   fetch → rebase → preflight → push → retry loop (`scripts/agent/push.sh`).
3. No force pushes, no history rewrites, no `git reset --hard` + push.
4. A change must not land unless the CGO preflight passes (`scripts/agent/preflight.sh`).
5. **CI on `main` must be green before the next agent pushes.** Direct-push raises broken-main risk;
   treat a red `main` as a stop-the-world event (§7).

## 2. Toolchain & preflight (Go + CGO)

IronClaw is Go 1.23+ with **`CGO_ENABLED=1`** (the SQLCipher encrypted-queue binding). The required
checks — wrapped by `scripts/agent/preflight.sh` — are:

```bash
export CGO_ENABLED=1
gofmt -l .          # format (must be empty)
go vet ./...        # lint / typecheck
go build ./...      # build
go test ./...       # tests   (equivalently: make build vet test)
```

There is **no** `npm`, `scripts/install.sh`, or JS tooling in this repo. Ignore those in the general
protocol.

## 3. Agent identity

Set a unique `AGENT_ID` (`<runtime>-<machine>-<short-random>`, e.g. `claude-host-4f8a`). It must appear
in claim comments, lock comments, and commit trailers:

```
Agent-ID: claude-host-4f8a
Task-Issue: #<n>
Base-SHA: <origin/main sha>
```

## 4. Task claiming (FCFS)

- **No issue, no work.** Every change ties to a GitHub Issue labelled `agent:ready`.
- **GitHub is the single source of truth for liveness.** A task is claimable iff its issue is **open,
  `agent:ready`, and has no live claim ref**. Find work with [`scripts/agent/board.sh`](scripts/agent/board.sh),
  not by reading the registry's `status` field. The registry
  ([`.agents/task-registry.json`](.agents/task-registry.json)) is the source for **deps, `owned_paths`,
  locks, and acceptance criteria only** — its per-task `status` is advisory and may lag reality.
- Claim the **lowest-wave, highest-priority** eligible task whose `depends_on` are all done and whose
  required locks are free. Claim exactly one at a time.
- **Claim atomically — never hand-roll the claim.** Run:

  ```bash
  AGENT_ID=<your-id> scripts/agent/claim.sh <issue-number> [scope...]
  ```

  This is the **only** safe way to claim. It wins the task by atomically creating a server-side claim
  ref (`refs/agent-claims/issue-<n>`) via GitHub's create-ref API — a compare-and-swap where exactly
  one racing agent gets the ref and every other gets rejected. Only the winner flips
  `agent:ready → agent:claimed + agent:in-progress` and posts the `/agent-claim` comment. If the script
  prints `ALREADY_CLAIMED` / `NOT_CLAIMABLE` / `RACE_LOST`, **the task is taken — pick another.** Do not
  add the labels or comment by hand; that path is the double-claim race this script exists to kill.
- Lease = 60 min, heartbeat every 20 min (`/agent-heartbeat`). To steal an expired lease, post a warning,
  then `scripts/agent/release.sh <issue> ready` (frees the claim ref) before re-claiming.
- **Scope is `owned_paths` first, with bounded expansion.** Do your work inside the task's
  `owned_paths`. When — and only when — a task's **acceptance criteria genuinely require** touching an
  adjacent package (e.g. a registry/host store a sandbox tool needs), you **may expand** to those
  files, subject to ALL of these:
  - **Never the frozen contract or any hard-lock path without its lock.** `internal/contract/**` stays
    RFC-gated + `agent:needs-human` (§5); `.github/**`/`Makefile`, `go.mod`/`go.sum`, `deploy/**` need
    their lock first.
  - **Never another task's actively-claimed `owned_paths`, nor a single-owner shared file**
    (`cmd/controlplane/main.go`, `AGENTS.md`, `.agents/task-registry.json`) without coordinating on the
    Coordination Board first.
  - **Stay minimal and task-scoped** — only the adjacent edits the acceptance demands, nothing more.
  - **Declare it.** Note the expansion in the `/agent-claim` comment and add an
    `Expanded-Scope: <paths> (why)` trailer to the commit.
  - If the needed expansion would hit a forbidden/contract/locked path you cannot take, **don't
    partial-land** — report the gap (`/agent-blocked`) or re-label `agent:needs-human` instead.
- **Verify before pushing.** `git diff --name-only origin/main...HEAD` must be within `owned_paths` plus
  any declared expansion; revert anything else.

## 5. Locks (IronClaw high-risk paths)

Acquire a hard lock (announce on the Coordination Board issue) before touching these. Lock order to
avoid deadlock: `lock:contract` → `lock:ci` → `lock:dependency` → `lock:release` → path locks. Every
lock has a TTL.

| Lock | Paths | Rule |
|---|---|---|
| **`lock:contract`** | `internal/contract/**` | **FROZEN.** Never auto-land. A contract change requires a joint RFC in `docs/contract.md` + both CODEOWNERS. If a task needs it, **stop and re-label `agent:needs-human`.** |
| `lock:ci` | `.github/**`, `Makefile` | one CI-config change at a time |
| `lock:dependency` | `go.mod`, `go.sum` | one dependency change at a time; use `go get <mod>@<ver>` + `go mod tidy`; do not switch toolchains |
| `lock:release` | `deploy/**` | one release/deploy change at a time |
| (path) | `cmd/controlplane/main.go`, `AGENTS.md`, `.agents/task-registry.json` | single-owner shared files; coordinate before editing |

The frozen contract is the central IronClaw invariant: a drift there is a silent decrypt/routing
failure at runtime, not a build error. Treat `lock:contract` as human-gated, always.

## 6. Commits

One issue per commit, small and reversible, tests included. Message format:

```
<area>: <short imperative summary>

Why:
- <reason>

What changed:
- <change>

Validation:
- CGO_ENABLED=1 go test ./...

Agent-ID: <id>
Task-Issue: #<n>
Base-SHA: <sha>
```

Do not mix refactors with features, do not run whole-repo `gofmt` (format touched files only), and do
not edit generated/contract files without the proper lock.

## 7. Broken-main protocol

If CI on `main` is red, a clean build fails, or the daemon won't start in `--dev`:

1. Announce `/agent-main-broken` on the Coordination Board; pause new pushes.
2. Identify the bad commit; `git revert <sha>` (never reset/force).
3. Run preflight; push the revert to restore green `main`.
4. Mark the originating issue `agent:reverted`; resume only when `main` is green.

## 8. Issue comments are machine-readable logs

Use `/agent-claim`, `/agent-heartbeat`, `/agent-progress`, `/agent-blocked`, `/agent-landed`,
`/agent-failed` (formats in the general protocol §20). If blocked > 5 min, post `/agent-blocked` and
switch to another unblocked task — never wait silently.

### 8.1 Landing is one atomic step — and it MUST close the issue

When your push lands on `main` and CI is green, finish the task with **one command**:

```bash
AGENT_ID=<your-id> scripts/agent/land.sh <issue-number> <commit-sha>
```

`land.sh` does the **entire** terminal transition so it can't be left half-done: it verifies the commit
is on `origin/main`, posts `/agent-landed`, swaps labels to `agent:done`, **closes the issue
(`gh issue close --reason completed`)**, and releases the claim ref. `agent:done` without a closed issue
is a bug — never stop at the label. If you abandon a claim instead of landing, run
`scripts/agent/release.sh <issue> ready|blocked|failed` so the claim ref is freed and the task isn't
stuck. Do **not** edit `.agents/task-registry.json` to mark a task done — GitHub issue state is
authoritative (§4); the registry status is regenerated, not hand-maintained.

## 9. Not applicable to this stack

The general protocol covers stacks IronClaw does not currently have. **Do not invent work for these**
(revisit only if the stack changes): DB migrations (§14), schema/proto/GraphQL/OpenAPI (§15), generated
clients (§16), per-agent test databases (§18.2), and the queue/integrator-bot machinery (§8/§9/§22).

## 10. Out of scope by threat model

Do **not** add these — they are intentional non-goals of IronClaw's sealed/`network=none` design, not
gaps to fill: in-sandbox web/browser access, `install_packages`/self-modification, a general
credential vault for arbitrary APIs, and multi-provider model backends. If a task seems to need one,
re-label `agent:needs-human`.

---

**The loop:** `board.sh → claim.sh (atomic) → lock → edit → preflight → push.sh (CAS) →
verify green → land.sh (close + done)`. The claim and the land are scripted and atomic on purpose —
hand-rolling either one is what causes double-claims and dangling open `agent:done` issues.
Any agent that cannot follow this loop must stop before changing code.
