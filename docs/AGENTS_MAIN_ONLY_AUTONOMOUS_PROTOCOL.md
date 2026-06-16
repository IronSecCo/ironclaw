# AGENTS.md — Autonomous Main-Only Multi-Agent GitHub Protocol

> Purpose: define the rules for multiple AI coding agents working automatically on the same remote GitHub open-source repository, across the same or different machines, **without human PR handling** and with all accepted changes landing directly on `main`.
>
> Place this file at the repository root as `AGENTS.md`. Every agent must read it before touching code.
>
> Human maintainer instructions override this file. When this file conflicts with a task instruction, this file wins unless a human maintainer explicitly says otherwise.

---

## 0. Operating Model

This repository uses a **main-only autonomous integration model**.

That means:

- Agents may run automatically.
- Agents do not open pull requests for normal work.
- Agents do not wait for human review unless the task is explicitly marked human-gated.
- Accepted changes land directly on `main`.
- Agents may use local branches or temporary queue branches as implementation details, but the project’s canonical working branch is always `main`.
- Only one integration process may write to `main` at a time.
- Agents must never force-push `main`.
- Agents must never overwrite another agent’s work to make progress.

The safest version of “run on main” is **not** “every agent pushes to `main` whenever it finishes.” That creates races and broken commits. The safe version is:

1. Agents read from `origin/main`.
2. Agents work locally in isolated clones, worktrees, containers, or machines.
3. Agents claim issues and file scopes in GitHub.
4. Agents submit finished commits to a serialized **main integration queue**.
5. A single **main integrator** lands one change at a time onto `main` after conflict checks and tests pass.

No PRs are required.

---

## 1. Hard Safety Invariants

These rules are non-negotiable.

### 1.1 `main` is the only shared truth

`origin/main` is the source of truth for all agents.

Before starting, before testing, and before submitting, each agent must fetch the latest state:

```bash
git fetch origin main --prune
```

An agent must assume its local view is stale unless it has just fetched.

### 1.2 Only the main integrator may write to `main`

Normal agents must not push directly to `main`.

Allowed:

```bash
git push origin HEAD:refs/heads/agent-queue/<issue-number>/<agent-id>
```

Not allowed for normal agents:

```bash
git push origin HEAD:main
git push --force origin main
git push --force-with-lease origin main
```

Only the main integrator bot, GitHub Action, or explicitly approved orchestrator may push to `main`.

### 1.3 No force pushes to `main`

Force pushing `main` is forbidden.

There is no exception for agents.

If `main` is broken, the recovery action is a revert commit, not history rewriting.

### 1.4 A failed integration must not land

A change must not land on `main` unless all required checks pass in the integration environment.

Minimum required checks:

- Format check.
- Lint check.
- Type check if applicable.
- Unit tests for touched packages.
- Build check if applicable.
- Repository-specific smoke test.

### 1.5 Broken `main` requires revert-first behavior

If a pushed commit breaks `main`, agents must not stack more fixes on top blindly.

The order is:

1. Stop new integrations.
2. Identify the bad commit.
3. Revert the bad commit.
4. Restore green `main`.
5. Re-attempt the fix from a fresh base.

Use:

```bash
git revert <bad_commit_sha>
```

Do not use:

```bash
git reset --hard <old_sha>
git push --force
```

---

## 2. Repository Configuration

The repository should be configured so that automatic agents can move fast but cannot easily destroy the project.

### 2.1 Recommended branch/ruleset policy

Configure `main` with a branch protection rule or repository ruleset.

Recommended settings:

- Block force pushes to `main`.
- Block deletion of `main`.
- Require signed commits if the project uses signing.
- Restrict direct pushes to the main integrator identity only.
- Allow the main integrator identity to bypass PR requirements if PR requirements exist.
- Require status checks for human-originated changes.
- Keep admins included unless there is a strong reason not to.

For this autonomous no-PR model, the important point is that **normal agents do not have direct write permission to `main`**. They can create queue refs. The main integrator is the only writer.

### 2.2 Use a bot identity for integration

Create one dedicated identity for landing changes.

Example names:

```txt
main-integrator[bot]
auto-main-writer
repo-agent-integrator
```

The bot should have:

- Write access to repository contents.
- Permission to comment on issues.
- Permission to read workflow results.
- No broad organization permissions beyond what is needed.

### 2.3 Required labels

Create these labels:

```txt
agent:ready
agent:claimed
agent:in-progress
agent:blocked
agent:needs-human
agent:done
agent:failed
agent:reverted
lock:high-risk
lock:dependency
lock:migration
lock:schema
lock:api-contract
lock:generated
lock:ci
lock:release
```

### 2.4 Required coordination issue

Create one permanent issue named:

```txt
Agent Coordination Board
```

Pin it.

This issue is used for:

- Global lock announcements.
- Main integration incidents.
- Broken-main reports.
- Deadlock recovery.
- Agent status heartbeats.

Example issue title:

```txt
[agent-coordination] Global coordination board
```

---

## 3. Agent Identity

Every agent must have a unique ID.

Format:

```txt
<agent-runtime>-<machine-or-run-id>-<short-random-id>
```

Examples:

```txt
codex-linux-4f8a
claude-codespace-b21c
openhands-macbook-a91d
autofix-ghaction-992e
```

The agent ID must appear in:

- Issue claim comments.
- Lock comments.
- Queue branch names.
- Commit trailers.
- Failure reports.

Recommended environment variable:

```bash
export AGENT_ID="codex-linux-4f8a"
```

Recommended commit trailer:

```txt
Agent-ID: codex-linux-4f8a
Task-Issue: #123
Base-SHA: abc1234
```

---

## 4. Task Claiming

### 4.1 No issue, no work

Every meaningful change must be tied to a GitHub Issue.

Agents must not invent untracked work. If an agent discovers additional work, it must create or update an issue.

### 4.2 Claim exactly one task at a time

An agent should claim one issue at a time unless it is explicitly running as an orchestrator.

Claim by posting a comment on the issue:

```txt
/agent-claim
agent_id: codex-linux-4f8a
base_sha: <current origin/main sha>
scope:
  - src/auth/**
  - tests/auth/**
non_scope:
  - package-lock.json
  - migrations/**
locks_requested: []
lease_minutes: 60
```

Then apply labels:

```txt
agent:claimed
agent:in-progress
```

If the issue is already claimed and the lease has not expired, do not work on it.

### 4.3 Claim lease

Every claim has a lease.

Default lease: 60 minutes.

The agent must refresh the lease every 20 minutes while active:

```txt
/agent-heartbeat
agent_id: codex-linux-4f8a
status: in-progress
latest_base_seen: <origin/main sha>
lease_extend_minutes: 60
notes: running tests
```

If a lease expires, another agent may claim the task after posting:

```txt
/agent-claim-expired
previous_agent: codex-linux-4f8a
new_agent: claude-codespace-b21c
reason: no heartbeat for 75 minutes
```

### 4.4 Scope is binding

The claimed scope is a contract.

An agent may edit only files inside its claimed scope unless it updates the issue comment and obtains additional locks if needed.

Before committing, the agent must verify its touched files:

```bash
git diff --name-only origin/main...HEAD
```

If any changed file is outside scope, the agent must either:

1. Revert the out-of-scope file, or
2. Update the claim and wait until there is no conflict with another active claim.

---

## 5. Locking Protocol

This repository uses three lock types.

### 5.1 Soft path claim

A soft path claim is used for normal files.

Example:

```txt
scope:
  - src/billing/invoices.ts
  - tests/billing/invoices.test.ts
```

Soft claims may overlap only when the files are clearly independent and the changes are additive.

If two agents need the same file, they must coordinate by issue comment before editing.

### 5.2 Hard lock

A hard lock is required for high-risk shared files.

Hard-lock paths:

```txt
AGENTS.md
.github/**
package.json
package-lock.json
pnpm-lock.yaml
yarn.lock
poetry.lock
requirements.txt
uv.lock
Pipfile.lock
go.mod
go.sum
Cargo.toml
Cargo.lock
Gemfile.lock
composer.lock
migrations/**
schema/**
openapi.*
proto/**
graphql/**
db/**
scripts/release/**
scripts/deploy/**
Dockerfile
docker-compose*.yml
.devcontainer/**
```

Hard lock request format:

```txt
/agent-lock-request
agent_id: codex-linux-4f8a
lock_type: dependency
paths:
  - package.json
  - package-lock.json
reason: add zod for config validation
base_sha: <origin/main sha>
lease_minutes: 30
```

Hard lock acquired format:

```txt
/agent-lock-acquired
agent_id: codex-linux-4f8a
lock_type: dependency
paths:
  - package.json
  - package-lock.json
expires_at: 2026-06-16T20:30:00Z
```

Hard lock release format:

```txt
/agent-lock-release
agent_id: codex-linux-4f8a
lock_type: dependency
paths:
  - package.json
  - package-lock.json
result: landed | abandoned | failed
```

### 5.3 Global main-writer lock

The main-writer lock is held only by the main integrator.

Agents do not acquire it directly.

The main integrator enforces this using GitHub Actions concurrency or an equivalent external queue.

Only one integration job may run at a time.

---

## 6. Deadlock Prevention

Deadlock happens when agents hold locks while waiting for each other.

To prevent it:

### 6.1 No nested locks unless ordered

If a task needs multiple locks, acquire them in this order:

1. `lock:ci`
2. `lock:dependency`
3. `lock:schema`
4. `lock:migration`
5. `lock:api-contract`
6. `lock:generated`
7. `lock:release`
8. path-specific locks

Never acquire a lower-priority lock and then wait for a higher-priority lock.

### 6.2 Lock TTL is mandatory

Every hard lock must have an expiry time.

Default TTLs:

```txt
dependency: 30 minutes
schema: 45 minutes
migration: 45 minutes
api-contract: 45 minutes
generated: 30 minutes
ci: 30 minutes
release: 60 minutes
```

An expired lock can be stolen after a warning comment.

### 6.3 No silent waiting

If blocked for more than 5 minutes, comment on the issue:

```txt
/agent-blocked
agent_id: codex-linux-4f8a
blocked_by: lock:dependency held by claude-codespace-b21c
since: 2026-06-16T20:00:00Z
next_action: retry after lock expires or switch task
```

Then do one of:

- Switch to another unblocked issue.
- Shrink scope.
- Ask for human help only if required.

---

## 7. Local Workspace Isolation

Agents may run on the same machine or different machines. Each active task must use an isolated workspace.

### 7.1 One workspace per task

Allowed:

```bash
git clone git@github.com:ORG/REPO.git repo-123-codex
```

Allowed:

```bash
git worktree add ../repo-123-codex origin/main
```

Not allowed:

- Two agents editing the same working tree.
- One agent running two tasks in the same working tree.
- Sharing uncommitted files across agents.

### 7.2 Local branch is allowed, remote PR is not required

Agents may create a local branch for sanity:

```bash
git checkout -B agent/123-codex-linux-4f8a origin/main
```

This is local only unless submitting to the queue.

### 7.3 Always clean before starting

Before starting work:

```bash
git status --short
```

If there are uncommitted changes, stop. Do not continue until the workspace is clean or the changes are intentionally carried forward from the same task.

---

## 8. Main Integration Queue

The queue is the core mechanism that makes no-PR main-only automation safe.

### 8.1 Queue branch naming

Agents submit finished work to a queue ref:

```txt
agent-queue/<issue-number>/<agent-id>
```

Example:

```txt
agent-queue/123/codex-linux-4f8a
```

This is not a PR branch. It is a transport ref for the integrator.

### 8.2 Submission requirements

Before submitting, the agent must:

```bash
git fetch origin main --prune
git rebase origin/main
./scripts/agent/preflight.sh
```

Then verify changed files:

```bash
git diff --name-only origin/main...HEAD
```

Then push to the queue:

```bash
ISSUE=123
QUEUE_REF="agent-queue/${ISSUE}/${AGENT_ID}"
git push origin HEAD:refs/heads/${QUEUE_REF}
```

Then trigger the integrator:

```bash
gh workflow run agent-main-integrator.yml \
  -f queue_ref="${QUEUE_REF}" \
  -f issue_number="${ISSUE}" \
  -f agent_id="${AGENT_ID}" \
  -f base_sha="$(git rev-parse origin/main)"
```

Then comment:

```txt
/agent-submit
agent_id: codex-linux-4f8a
queue_ref: agent-queue/123/codex-linux-4f8a
base_sha: <origin/main sha>
local_checks: passed
changed_files:
  - src/auth/session.ts
  - tests/auth/session.test.ts
```

### 8.3 Integration behavior

The main integrator must:

1. Acquire the global main-writer lock.
2. Fetch latest `origin/main`.
3. Fetch the queue ref.
4. Verify the queue ref descends from the declared base SHA or can be cherry-picked cleanly.
5. Apply the agent changes onto current `main`.
6. Run required tests.
7. Commit or preserve the agent commit with clear metadata.
8. Push to `main` with a normal fast-forward push.
9. Delete the queue ref after successful landing.
10. Comment result on the issue.

### 8.4 Integration failure behavior

If integration fails due to conflicts:

- Do not push to `main`.
- Comment on the issue with the conflicting files.
- Mark issue `agent:blocked` or `agent:failed`.
- Keep or delete the queue ref according to repo policy.

If integration fails due to tests:

- Do not push to `main`.
- Comment with failing commands and logs.
- Return the task to the original agent if it is alive.
- Otherwise release the claim after the lease expires.

### 8.5 No queue skipping

Agents must not bypass the queue, even for “small” changes.

Small changes are exactly where accidental races happen.

---

## 9. Recommended GitHub Action: Main Integrator

Create:

```txt
.github/workflows/agent-main-integrator.yml
```

Example:

```yaml
name: Agent Main Integrator

on:
  workflow_dispatch:
    inputs:
      queue_ref:
        description: "Queue branch, e.g. agent-queue/123/codex-linux-4f8a"
        required: true
        type: string
      issue_number:
        description: "GitHub issue number"
        required: true
        type: string
      agent_id:
        description: "Submitting agent id"
        required: true
        type: string
      base_sha:
        description: "origin/main SHA observed by the agent before submission"
        required: true
        type: string

permissions:
  contents: write
  issues: write
  actions: read

concurrency:
  group: main-writer
  cancel-in-progress: false

jobs:
  integrate:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout main
        uses: actions/checkout@v4
        with:
          ref: main
          fetch-depth: 0

      - name: Configure Git identity
        run: |
          git config user.name "main-integrator[bot]"
          git config user.email "main-integrator[bot]@users.noreply.github.com"

      - name: Fetch queue ref
        run: |
          git fetch origin "refs/heads/${{ inputs.queue_ref }}:refs/remotes/origin/${{ inputs.queue_ref }}"

      - name: Verify base exists in submitted history
        run: |
          git merge-base --is-ancestor "${{ inputs.base_sha }}" "refs/remotes/origin/${{ inputs.queue_ref }}"

      - name: Apply submitted commits onto latest main
        run: |
          git checkout -B integration origin/main
          git cherry-pick --no-commit "${{ inputs.base_sha }}..refs/remotes/origin/${{ inputs.queue_ref }}"

      - name: Show changed files
        run: |
          git diff --name-only --cached

      - name: Install dependencies
        run: ./scripts/install.sh

      - name: Preflight
        run: ./scripts/agent/preflight.sh

      - name: Commit integrated change
        run: |
          git commit -m "agent: integrate issue #${{ inputs.issue_number }}" \
            -m "Agent-ID: ${{ inputs.agent_id }}" \
            -m "Task-Issue: #${{ inputs.issue_number }}" \
            -m "Queue-Ref: ${{ inputs.queue_ref }}" \
            -m "Base-SHA: ${{ inputs.base_sha }}"

      - name: Push to main
        run: |
          git push origin HEAD:main

      - name: Delete queue ref
        if: success()
        run: |
          git push origin --delete "${{ inputs.queue_ref }}" || true

      - name: Comment success
        if: success()
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh issue comment "${{ inputs.issue_number }}" --body "/agent-landed
          agent_id: ${{ inputs.agent_id }}
          queue_ref: ${{ inputs.queue_ref }}
          result: landed_on_main"

      - name: Comment failure
        if: failure()
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh issue comment "${{ inputs.issue_number }}" --body "/agent-integration-failed
          agent_id: ${{ inputs.agent_id }}
          queue_ref: ${{ inputs.queue_ref }}
          result: failed_before_main_push
          action_required: rebase_or_fix_tests"
```

Repository-specific projects should replace the install and preflight steps with exact project commands.

---

## 10. Preflight Script

Create:

```txt
scripts/agent/preflight.sh
```

Minimum template:

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "== Agent preflight =="

echo "Git SHA: $(git rev-parse HEAD)"
echo "Base main: $(git rev-parse origin/main 2>/dev/null || true)"

echo "== Changed files =="
git diff --name-only origin/main...HEAD || true

if [ -x ./scripts/format-check.sh ]; then
  ./scripts/format-check.sh
fi

if [ -x ./scripts/lint.sh ]; then
  ./scripts/lint.sh
fi

if [ -x ./scripts/typecheck.sh ]; then
  ./scripts/typecheck.sh
fi

if [ -x ./scripts/test.sh ]; then
  ./scripts/test.sh
fi

if [ -x ./scripts/build.sh ]; then
  ./scripts/build.sh
fi

echo "== Preflight passed =="
```

Make executable:

```bash
chmod +x scripts/agent/preflight.sh
```

---

## 11. Direct Push Mode — Only If You Reject the Queue

Direct-to-main from every agent is not recommended.

If the project still chooses direct push mode, all agents must follow this compare-and-swap loop:

```bash
MAX_RETRIES=3
for attempt in $(seq 1 $MAX_RETRIES); do
  git fetch origin main --prune
  git rebase origin/main
  ./scripts/agent/preflight.sh

  if git push origin HEAD:main; then
    echo "Pushed to main"
    exit 0
  fi

  echo "Push rejected. main moved. Retrying after rebase. Attempt ${attempt}/${MAX_RETRIES}."
  sleep $((attempt * 10))
done

echo "Failed to push after retries. Marking blocked."
exit 1
```

Rules for direct push mode:

- Never use force push.
- Never push if tests were not run after the latest rebase.
- Never push if changed files exceed claimed scope.
- If push is rejected, fetch, rebase, retest, retry.
- After three failed retries, mark blocked and release the task.

Again: queue mode is preferred.

---

## 12. Conflict Handling

### 12.1 Conflict during local rebase

When `git rebase origin/main` conflicts:

1. Stop editing.
2. Inspect the conflict.
3. Determine whether the conflict is inside claimed scope.
4. If outside scope, abort and mark blocked.
5. If inside scope, resolve manually and run tests.

Allowed:

```bash
git status
git diff
```

Allowed after careful review:

```bash
git add <resolved-file>
git rebase --continue
```

Not allowed:

```bash
git checkout --ours .
git checkout --theirs .
git merge -X ours
git merge -X theirs
```

Blanket conflict resolution is forbidden.

### 12.2 Conflict in the main integrator

If the integrator cannot cherry-pick cleanly, it must not push.

It should comment:

```txt
/agent-integration-conflict
agent_id: codex-linux-4f8a
queue_ref: agent-queue/123/codex-linux-4f8a
conflicts:
  - src/auth/session.ts
recommended_action: fetch latest main, rebase locally, rerun tests, resubmit
```

### 12.3 Semantic conflict

A semantic conflict happens when Git merges cleanly but behavior is incompatible.

Examples:

- Two agents add different validation rules for the same API.
- One agent changes an interface while another adds a caller using old assumptions.
- Two agents add migrations that conflict in order or naming.

Prevention:

- Hard-lock schemas, migrations, generated clients, and API contracts.
- Require tests that cross package boundaries.
- Run integration tests in the main integrator, not only locally.

---

## 13. Dependency Changes

Dependency changes are high-risk because they touch shared lockfiles.

Rules:

- Acquire `lock:dependency` before editing dependency manifests or lockfiles.
- Do not reformat or regenerate unrelated lockfile sections.
- Use the repository’s package manager only.
- Do not switch package managers.
- Do not upgrade broad dependency ranges unless the issue asks for it.
- Include the exact install command in the issue comment.

Example comment:

```txt
/agent-dependency-change
agent_id: codex-linux-4f8a
command: npm install zod@3.23.8
files:
  - package.json
  - package-lock.json
reason: runtime config validation for issue #123
```

---

## 14. Database Migrations

Migration changes require `lock:migration`.

Rules:

- One migration-producing task at a time.
- Migration filename must include timestamp or monotonic sequence according to repo convention.
- Never edit an already-landed migration unless the issue explicitly says to repair it.
- Prefer additive migrations.
- Include rollback if the project convention requires rollback.
- Run migration tests before submission.

Example:

```txt
/agent-lock-request
agent_id: codex-linux-4f8a
lock_type: migration
paths:
  - migrations/**
reason: add user_sessions table for issue #123
lease_minutes: 45
```

---

## 15. API Contracts, Schemas, Proto, GraphQL, OpenAPI

Contract changes require `lock:api-contract` or `lock:schema`.

Rules:

- Update contract source first.
- Regenerate clients using checked-in generator commands.
- Include generated files only if the repository normally commits them.
- Update tests for both producer and consumer when possible.
- Comment with compatibility impact.

Example:

```txt
/agent-contract-change
agent_id: codex-linux-4f8a
compatibility: backward-compatible
producer_updated: true
consumer_updated: true
generator_command: npm run generate:api
```

---

## 16. Generated Files

Generated files are dangerous in parallel workflows.

Rules:

- Acquire `lock:generated` before regenerating large generated outputs.
- Do not manually edit generated files.
- Run the generator from a clean tree.
- Commit source and generated output together.
- If generated output changes unexpectedly, stop and investigate.

---

## 17. Formatting

Formatting can cause unnecessary conflicts.

Rules:

- Do not run whole-repository formatting unless the issue explicitly asks for formatting.
- Format touched files only.
- Do not mix formatting-only changes with logic changes unless unavoidable.
- If formatting config changes, acquire `lock:ci` or relevant config lock.

Allowed:

```bash
npx prettier --write src/auth/session.ts tests/auth/session.test.ts
```

Avoid:

```bash
npx prettier --write .
```

---

## 18. Tests and Shared Resources

### 18.1 Ports

Agents must not assume fixed local ports are available.

Use dynamic ports where possible.

If fixed ports are required, derive them from `AGENT_ID` or task number.

Example:

```bash
export TEST_PORT=$((3000 + ISSUE_NUMBER % 1000))
```

### 18.2 Databases

Each agent must use an isolated test database.

Recommended naming:

```txt
repo_test_<issue_number>_<agent_id>
```

No agent may drop, reset, or mutate a shared database unless the task owns that environment.

### 18.3 External services

Agents must not spend money, send real emails, call production APIs, or mutate production state unless explicitly approved.

Use mocks, local emulators, or staging credentials.

---

## 19. Commit Rules

### 19.1 Commit size

Each integration should be small enough to revert safely.

Preferred:

- One issue per integrated commit.
- One logical change per issue.
- Tests included with code.

Avoid:

- Large unrelated refactors.
- Drive-by cleanup.
- Formatting unrelated files.

### 19.2 Commit message

Use this format:

```txt
<area>: <short imperative summary>

Why:
- <reason>

What changed:
- <change 1>
- <change 2>

Validation:
- <test command 1>
- <test command 2>

Agent-ID: <agent-id>
Task-Issue: #<issue-number>
Base-SHA: <sha>
```

Example:

```txt
auth: validate expired sessions before refresh

Why:
- Prevent refresh attempts for already-expired sessions.

What changed:
- Added expiry guard in session refresh path.
- Added unit coverage for expired refresh attempts.

Validation:
- npm run lint
- npm test -- session

Agent-ID: codex-linux-4f8a
Task-Issue: #123
Base-SHA: 6c9f0aa
```

---

## 20. Issue Comments as Machine-Readable Logs

Agents must leave structured comments that other agents can parse.

### 20.1 Start

```txt
/agent-start
agent_id: codex-linux-4f8a
issue: 123
base_sha: <sha>
scope:
  - src/auth/**
  - tests/auth/**
```

### 20.2 Progress

```txt
/agent-progress
agent_id: codex-linux-4f8a
issue: 123
status: implemented
changed_files:
  - src/auth/session.ts
  - tests/auth/session.test.ts
next: running preflight
```

### 20.3 Blocked

```txt
/agent-blocked
agent_id: codex-linux-4f8a
issue: 123
blocked_by: lock:dependency
owner: claude-codespace-b21c
since: 2026-06-16T20:00:00Z
next: waiting until lock expiry, then retry
```

### 20.4 Submitted

```txt
/agent-submit
agent_id: codex-linux-4f8a
issue: 123
queue_ref: agent-queue/123/codex-linux-4f8a
base_sha: <sha>
checks: passed
```

### 20.5 Landed

```txt
/agent-landed
agent_id: codex-linux-4f8a
issue: 123
commit: <sha>
result: landed_on_main
```

### 20.6 Failed

```txt
/agent-failed
agent_id: codex-linux-4f8a
issue: 123
stage: preflight | integration | tests | push
reason: <short reason>
logs: <link or summary>
next: <recommended action>
```

---

## 21. Autonomous Agent Runtime Prompt

Use this as the system/developer prompt for every coding agent.

```txt
You are an autonomous AI coding agent working on a shared open-source GitHub repository with other agents.

The repository uses a no-PR, main-only integration model. You may work locally, but you must not push directly to main unless you are the designated main integrator. Normal agents submit completed work to the main integration queue.

Your prime directive is to make correct progress without corrupting main, blocking other agents, overwriting work, or creating hidden coordination state.

You must follow AGENTS.md exactly.

Before work:
1. Set AGENT_ID if missing.
2. Fetch latest origin/main.
3. Ensure your working tree is clean.
4. Select one issue labeled agent:ready.
5. Claim the issue with a structured /agent-claim comment.
6. Define exact file scope.
7. Acquire hard locks for dependency files, migrations, schemas, API contracts, generated files, CI/CD, deployment scripts, Docker/devcontainer files, or AGENTS.md.

During work:
1. Stay inside claimed scope.
2. Use an isolated workspace for this task.
3. Do not touch high-risk files without a hard lock.
4. Do not run whole-repo formatting unless explicitly requested.
5. Leave heartbeat comments every 20 minutes for long tasks.
6. If blocked for more than 5 minutes, post /agent-blocked and switch tasks or stop.
7. Never wait silently.

Before submission:
1. Fetch origin/main.
2. Rebase on origin/main.
3. Resolve conflicts manually and only inside scope.
4. Run ./scripts/agent/preflight.sh.
5. Verify changed files with git diff --name-only origin/main...HEAD.
6. Ensure all changes are tied to the issue.
7. Push to agent-queue/<issue>/<AGENT_ID>, not main.
8. Trigger the main integrator workflow.
9. Post /agent-submit with queue_ref, base_sha, changed files, and test results.

Never:
- Never push directly to main as a normal agent.
- Never force-push main.
- Never reset main.
- Never overwrite conflicts using blanket ours/theirs.
- Never edit outside claimed scope.
- Never hold a lock without TTL.
- Never keep working after discovering main is broken.
- Never hide failures.

If main is broken:
1. Stop new work.
2. Identify the bad commit.
3. Revert first.
4. Restore green main.
5. Re-attempt from fresh origin/main.

Output expectations:
- Keep issue comments structured and machine-readable.
- Keep commits small and reversible.
- Prefer boring, deterministic changes over clever changes.
- Optimize for safe autonomous throughput.
```

---

## 22. Main Integrator Runtime Prompt

Use this only for the designated main integrator bot or workflow.

```txt
You are the main integrator for an autonomous multi-agent GitHub repository.

You are the only process allowed to write to main.

Your job is to serialize agent submissions, apply them onto the latest main, run required checks, and push only safe changes.

For each submitted queue ref:
1. Acquire the global main-writer lock.
2. Fetch latest origin/main.
3. Fetch the queue ref.
4. Verify the submitted branch descends from the declared base SHA.
5. Cherry-pick the submitted commits onto latest main without committing.
6. If conflicts occur, stop and comment /agent-integration-conflict. Do not push.
7. Run the full integration preflight.
8. If checks fail, stop and comment /agent-integration-failed. Do not push.
9. Commit the integrated change with Agent-ID, Task-Issue, Queue-Ref, and Base-SHA trailers.
10. Push to main with a normal non-force push.
11. If push is rejected because main moved, fetch latest main, retry from step 5 up to three times.
12. Delete the queue ref after success.
13. Comment /agent-landed on the issue.

Never:
- Never force-push main.
- Never bypass tests.
- Never land conflicting changes.
- Never land changes that edit files outside the agent’s claimed scope.
- Never land a task marked agent:needs-human.
- Never continue integrating if main is already red.

If a landed commit breaks main:
1. Pause the queue.
2. Revert the bad commit.
3. Push the revert.
4. Mark the original issue agent:reverted.
5. Resume only when main is green.
```

---

## 23. Orchestrator Rules

If an orchestrator assigns work to agents, it must follow these rules.

### 23.1 Task slicing

Good task:

```txt
Issue #123: Add validation for auth session expiry
Scope:
- src/auth/session.ts
- tests/auth/session.test.ts
No dependency changes.
No schema changes.
```

Bad task:

```txt
Improve auth system
```

Tasks should be small, scoped, and independently testable.

### 23.2 Avoid overlapping scopes

The orchestrator should not assign two active tasks that modify the same files.

Before assigning, search active issues for:

```txt
agent:claimed
agent:in-progress
lock:*
```

### 23.3 Prefer vertical slices

Prefer a small complete change over a broad horizontal refactor.

Good:

```txt
Add rate-limit error handling to one endpoint with tests.
```

Bad:

```txt
Refactor all API errors across the project.
```

### 23.4 Escalate human-gated tasks

Mark these as `agent:needs-human` unless explicitly approved:

- License changes.
- Security policy changes.
- Public API breaking changes.
- Data deletion behavior.
- Production deployment changes.
- Authentication or authorization model changes.
- Payment logic changes.
- Legal/compliance text.
- Large dependency upgrades.

---

## 24. Broken Main Protocol

### 24.1 Detect

Main is considered broken if:

- Required CI fails on `main`.
- Build fails from clean checkout.
- Tests fail from clean checkout.
- App cannot start in default dev mode.
- A critical smoke test fails.

### 24.2 Announce

Post on the coordination issue:

```txt
/agent-main-broken
reported_by: codex-linux-4f8a
main_sha: <sha>
failing_check: test
suspected_commit: <sha>
impact: blocks integration queue
```

Apply label:

```txt
agent:blocked
```

### 24.3 Pause integrations

The main integrator must stop accepting queue submissions until main is green.

### 24.4 Revert

Prefer reverting the suspected bad commit:

```bash
git fetch origin main
git checkout -B revert-main origin/main
git revert <bad_commit_sha>
./scripts/agent/preflight.sh
git push origin HEAD:main
```

### 24.5 Resume

After main is green:

```txt
/agent-main-restored
restored_by: main-integrator[bot]
revert_commit: <sha>
queue_status: resumed
```

---

## 25. Minimal File Layout for This Protocol

Recommended repository additions:

```txt
AGENTS.md
.github/workflows/agent-main-integrator.yml
scripts/agent/preflight.sh
scripts/agent/submit.sh
scripts/agent/claim.sh
scripts/agent/locks.sh
```

Optional:

```txt
.agent/README.md
.agent/task-template.md
.agent/lock-policy.md
.agent/orchestrator-config.yml
```

Do not store live locks only in repo files because repo-file locks require commits and can themselves conflict. Use GitHub issues/comments/labels or an external transactional store.

---

## 26. Optional: Simple Agent Submit Script

Create:

```txt
scripts/agent/submit.sh
```

Example:

```bash
#!/usr/bin/env bash
set -euo pipefail

: "${AGENT_ID:?AGENT_ID is required}"
: "${ISSUE_NUMBER:?ISSUE_NUMBER is required}"

git fetch origin main --prune
git rebase origin/main

./scripts/agent/preflight.sh

CHANGED_FILES=$(git diff --name-only origin/main...HEAD)
if [ -z "$CHANGED_FILES" ]; then
  echo "No changes to submit."
  exit 0
fi

QUEUE_REF="agent-queue/${ISSUE_NUMBER}/${AGENT_ID}"
BASE_SHA="$(git rev-parse origin/main)"

git push origin HEAD:refs/heads/${QUEUE_REF}

gh workflow run agent-main-integrator.yml \
  -f queue_ref="${QUEUE_REF}" \
  -f issue_number="${ISSUE_NUMBER}" \
  -f agent_id="${AGENT_ID}" \
  -f base_sha="${BASE_SHA}"

gh issue comment "${ISSUE_NUMBER}" --body "/agent-submit
agent_id: ${AGENT_ID}
issue: ${ISSUE_NUMBER}
queue_ref: ${QUEUE_REF}
base_sha: ${BASE_SHA}
checks: passed
changed_files:
$(printf '%s\n' "$CHANGED_FILES" | sed 's/^/  - /')"
```

Make executable:

```bash
chmod +x scripts/agent/submit.sh
```

---

## 27. Final Rule

Autonomy is allowed. Silent uncoordinated writes are not.

The goal is not to imitate human PR review. The goal is to replace manual PR handling with deterministic coordination:

```txt
claim -> lock -> edit -> test -> queue -> serialize -> integrate -> verify -> land -> report
```

Any agent that cannot follow this loop must stop before changing code.
