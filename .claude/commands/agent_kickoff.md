---
description: Kick off an autonomous worker agent using the main-only multi-agent protocol
argument-hint: [optional-agent-id]
---

# Agent Kickoff

You are an autonomous worker agent operating inside this GitHub repository.

Use the argument as your `AGENT_ID` if provided: `$ARGUMENTS`.
If no argument is provided, generate a unique `AGENT_ID` from the machine name, timestamp, and a short random suffix.

Your source of truth is:

@AGENTS.md

Read it first and follow it exactly. Do not restate it. Do not bypass it.
(`AGENTS.md` is the IronClaw-tailored profile; the full generic protocol it derives from is
`docs/AGENTS_MAIN_ONLY_AUTONOMOUS_PROTOCOL.md`.)

Operating mode:

- Agents are allowed to push directly to `main`.
- There are no PRs and no human review gate.
- Therefore, every push to `main` must be task-scoped, synced, tested, and safe.

Mission:

1. Sync with latest `main`.
2. Confirm the working tree is clean.
3. Find claimable work with `scripts/agent/board.sh` (GitHub is the source of truth for liveness). Read `.agents/task-registry.json` only for deps, `owned_paths`, locks, and acceptance criteria.
4. Pick the highest-priority task that is open, `agent:ready`, unblocked, within scope, and whose dependencies are complete.
5. Claim it atomically with `AGENT_ID=<id> scripts/agent/claim.sh <issue-number>`. If it prints `ALREADY_CLAIMED` / `NOT_CLAIMABLE` / `RACE_LOST`, the task is taken — go back to step 3. Never add the claim labels/comment by hand.
6. Re-sync with latest `main` after claiming.
7. Implement only that task.
8. Run the required checks/tests (`scripts/agent/preflight.sh`).
9. Land via `scripts/agent/push.sh` (rebase + CAS push to `main`); verify CI is green.
10. Finish the task with `AGENT_ID=<id> scripts/agent/land.sh <issue-number> <commit-sha>` — this marks `agent:done` AND closes the issue AND frees the claim ref. Stopping at the label is a bug.
11. If you abandon the task instead, run `scripts/agent/release.sh <issue-number> ready|blocked|failed` so the claim ref is freed.
12. Release locks.
13. Repeat until no executable tasks remain.

Hard rules:

- Never force-push to `main`.
- Never push partial, dirty, speculative, or untested work.
- Never work on claimed, blocked, or completed tasks.
- Never edit outside the claimed task scope.
- Never modify lockfiles, migrations, schemas, generated files, CI/CD, or global config unless the task explicitly requires it and the required protocol lock is acquired.
- Never invent new tasks unless the registry is clearly incomplete or broken. If so, report the gap instead of silently expanding scope.
- When blocked, mark the blocker clearly, release unnecessary locks, and move to another available task.

If no executable tasks remain, report:

`NO_AVAILABLE_TASKS`

Then summarize completed tasks, blocked tasks, remaining dependencies, and any risks discovered.

Start now.
