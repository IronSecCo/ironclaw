---
title: PR review and the machine reviewer of record
description: How IronClaw records pull-request review decisions with a GitHub App reviewer of record, the automatable config that enforces it, and the one-time human setup step.
---

# PR review & the machine reviewer of record

> **Status:** decision recorded; automatable config landed. One irreducible
> human step remains (create/install the reviewer GitHub App) — see
> [Human handoff](#human-handoff-one-time-setup).

## Why this exists

`main` is protected by a branch ruleset
([`.github/rulesets/main.json`](https://github.com/IronSecCo/ironclaw/blob/main/.github/rulesets/main.json))
that requires **one approving review** plus the `build` and `CodeQL` status
checks before any PR can merge. That is the gate that keeps unreviewed code off
`main`.

Today every IronClaw agent (Forge, Relay, QA, …) drives Git/GitHub under the
**single `omerzamir` GitHub identity**. GitHub will not let an identity approve
its own pull request. So when an agent opens a PR as `omerzamir`, *no agent* can
post the approving review the ruleset requires — the author and every available
reviewer are the same actor. Security-critical PRs that have a recorded
Paperclip review of record still cannot satisfy the GitHub gate, and the only
escape valve is an admin bypass (the repo-admin `bypass_actors` entry).

Admin bypass is a band-aid: it lets a green PR merge **without any second-actor
approval being recorded on GitHub at all**, which is exactly the property the
gate exists to guarantee. This document is the durable fix: a **distinct,
trusted reviewer actor** that is not the PR author, so the required-review gate
is satisfied honestly — no bypass.

## What "review of record" means here

The judgement — *should this change merge?* — is made and recorded in
**Paperclip** (a board approval, a QA sign-off, or an execution-policy review
stage). That is the review of record. GitHub's approving review is a
**mechanical reflection** of that decision by a distinct actor, so branch
protection can verify "author ≠ approver" cryptographically.

The reviewer actor is therefore **not** a rubber stamp that auto-approves every
PR. It only approves when a human/board review of record already exists and is
referenced. The gating lives in *who can trigger the approval* and *what they
must supply*, never in the actor approving unconditionally.

## Options considered

| | **Option A — GitHub App** *(recommended)* | **Option B — dedicated bot user** |
|---|---|---|
| Distinct actor for the gate | ✅ Approvals post as `app-slug[bot]`, a different actor than `omerzamir`. App reviews count toward `required_approving_review_count`. | ✅ A second user account is a different actor; its approval counts. |
| Can be a CODEOWNER | ❌ CODEOWNERS only accepts users/teams, not apps. (Our ruleset has `require_code_owner_review: false`, so this is not needed for the gate.) | ✅ Can be listed in CODEOWNERS and required via `require_code_owner_review`. |
| Seats / cost | ✅ Apps consume **no seat**. Free. | ⚠️ Free on this **public** repo, but a paid org seat per private repo; account lifecycle (email, 2FA, recovery) to own forever. |
| Secret handling | ⚠️ One long-lived **App private key** (PEM), stored as a repo/org Actions secret, scoped to `pull_requests: write` + `contents: write` (required so the approval counts) + `metadata: read`. Rotatable; never touches release signing (which stays keyless/OIDC). | ❌ A full second login: password + 2FA + a long-lived PAT with `repo` scope. Larger, human-shaped attack surface. |
| Audit story | ✅ Reviews are clearly machine-posted by a named bot; fine-grained, least-privilege permissions; one auditable installation. | ⚠️ Looks like a human; easy to over-grant; harder to reason about least privilege. |
| Automatable now | ✅ Manifest + workflow scaffolded in this repo; only App creation/install needs a human. | ⚠️ Account creation needs a human inbox + 2FA; collaborator invite + CODEOWNERS automatable after. |
| GitHub guidance | ✅ Apps are GitHub's recommended automation primitive. | ⚠️ Machine user accounts are allowed but discouraged when an App fits. |

### Recommendation: **Option A — a dedicated GitHub App.**

It gives a distinct reviewer actor that satisfies the *existing* ruleset with
**no branch-protection change**, consumes no seat, carries the smallest,
fine-grained permission set, and produces the cleanest audit trail. Its only
cost is a single long-lived CI credential (the App private key) — a scoped,
rotatable Actions secret that never participates in release signing, so the
keyless/OIDC signing posture is unchanged.

CODEOWNERS (which an App cannot satisfy) is kept as a **ready-to-activate**
artifact for the Option-B fallback and for documenting ownership; it stays
advisory while `require_code_owner_review` is `false`.

## How the gate is satisfied (Option A flow)

```
agent opens PR as omerzamir ──► CI: build + CodeQL go green
                                      │
                  human/board review of record recorded in Paperclip
                                      │
        repo admin dispatches reviewer-approve.yml with { pr, review_of_record }
                                      │
   workflow: admin-actor check ─► mint App installation token ─► POST APPROVE
                                      │
       branch protection sees 1 approval from ironclaw-reviewer[bot] ≠ author
                                      │
                              PR is mergeable — no admin bypass
```

The mechanics live in
[`.github/workflows/reviewer-approve.yml`](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/reviewer-approve.yml):

- **Trigger:** `workflow_dispatch` only — never runs on push/PR, so it cannot
  rubber-stamp anything automatically and adds no required-check surface.
- **Inputs:** `pr` (number) and `review_of_record` (the Paperclip approval id or
  URL). The approval body embeds `review_of_record` so the GitHub approval links
  back to the recorded decision.
- **Authorization:** the workflow re-checks that the dispatching actor has
  **admin** permission on the repo before approving (dispatch already requires
  write; the explicit admin check tightens it to repo admins, e.g. the CEO).
- **Least privilege:** the job's `GITHUB_TOKEN` is `contents: read` +
  `pull-requests: read`; the approving review is posted with the **App
  installation token** (`pull_requests: write`), minted at run time via
  [`actions/create-github-app-token`](https://github.com/actions/create-github-app-token)
  (SHA-pinned). The App is identified by `vars.REVIEWER_APP_CLIENT_ID` (the App's
  Client ID — a non-secret identifier, useless without the key) and the App private
  key is read from `secrets.REVIEWER_APP_PRIVATE_KEY` and masked by the action — it is
  never logged.
- **Safety:** the workflow refuses to approve if the App secret is absent, if
  the PR is not open, or if the PR author is the App itself — except the one
  recognised formula-bump case below, where it exits cleanly with an
  admin-merge runbook rather than a red run.

Until the App is installed and both the Client ID variable and the private-key
secret exist, the workflow is **inert**
(a manual dispatch fails fast with a clear message). Nothing in CI or the
release path depends on it, so landing the scaffold changes no current behaviour.

## Homebrew formula bump PRs (the one self-authored case)

The Release workflow's `formula` job (IRO-204) opens the rolling `brew/track` PR
(one long-lived branch, force-reset to the release tip each cut — IRO-270; older
releases used a per-tag `brew/track-<tag>` branch) that points
`brew install ironsecco/ironclaw/ironclaw` at each new release. Per
[IRO-203](https://github.com/IronSecCo/ironclaw/issues) the repo keeps *"Allow
GitHub Actions to create and approve pull requests"* **off**, so the default
`GITHUB_TOKEN` cannot open that PR; the job opens it with a scoped reviewer-App
token instead. That makes the **reviewer App the PR author**.

GitHub will not let an actor approve its own PR, so the reviewer App can never
approve its own bump PR — and it should not need to. The bump PR is:

- **generated** (machine-written by `scripts/update-homebrew-formula.sh`),
- **formula-only** (it touches `Formula/ironclaw.rb` and nothing else), and
- **pinned to the release's signed `SHA256SUMS`** — the cosign-signed checksum
  set IS the review of record; the formula merely transcribes those hashes.

To avoid a misleading red `reviewer-approve` run, that workflow recognises this
narrow case — PR author == reviewer App **and** head branch `brew/track` (or a
legacy `brew/track-*`) **and** the diff is exactly `Formula/ironclaw.rb` — and
exits cleanly with a pointer to this section instead of failing. **Every other
self-authored PR still hard-fails:** the gate is unchanged for product code.

### The bump branch is written by the App, not by `GITHUB_TOKEN` (IRO-677)

The `formula` job used to open the PR with the App token but write the branch with
the default `GITHUB_TOKEN`. That split cost a **second** human click on more than a
third of bumps, and a worse one than the approving review: it stopped the required
checks from *reporting* at all.

GitHub arms the workflow-approval gate on the identity that **wrote the head ref**,
not on the PR author. Opening the rolling PR pushes nothing, so the `opened` event's
actor is the App — a real contributor here — and CI just runs. Force-pushing the
*next* bump onto that still-open PR fires `synchronize` with actor
`github-actions[bot]`, which has no merged PR in this repo and does not appear in
`/contributors`, so every run lands in `action_required`. A parked run never
reports, so `build`, `CodeQL` and `brew-formula-verify` were simply **missing** and
the PR sat at "waiting for status" indefinitely (#614).

Measured over every run ever on `brew/track`: **165 of 165** `action_required` runs
were written by `github-actions[bot]`; **0 of 352** App-written runs stalled. A
controlled two-arm probe (one virgin App-opened PR per arm, identical REST calls,
identical pinned commit author, only the token differing) reproduced it on demand —
`GITHUB_TOKEN` arm 3/3 `action_required`, App arm 0/3.

So the job now mints the App token with `contents: write` and uses it for the ref
force-reset and the Contents commit as well as for `gh pr create`. Things this
deliberately does **not** do:

- it does not touch `fork-pr-contributor-approval`, which stays
  `first_time_contributors_new_to_github` and still gates every genuine fork PR;
- it does not add a credential — the App and its two secrets already existed, and
  this replaces `GITHUB_TOKEN`'s `contents: write` on the same two writes rather
  than adding a writer;
- it does not widen reach into `main`. The App is **not** in the ruleset's
  `bypass_actors` (only the admin repository role is), so main still requires a
  reviewed PR, passing required checks and signed commits;
- it does not change the commit. The `author` pin is what makes the head commit
  unsigned, and that is independent of the token, so `license/cla` still passes and
  the squash still mints main's signature — see *Squash only* below.

Two consequences worth knowing before you read a run list:

- **`push`-triggered workflows now actually fire on `brew/track`.** They never did
  before: GitHub suppresses workflow runs for `GITHUB_TOKEN` pushes entirely, so
  `brew-formula-verify`'s `push: branches: [brew/**]` trigger was **vacuous** for
  ~90 bumps. Expect one extra `CI` and one extra `brew-formula-verify` run per bump.
  They do not fight the PR runs: `brew-formula-verify`'s concurrency group keys on
  `pull_request.number || github.ref`, so push and PR land in different groups, and
  `ci.yml` has no concurrency group at all.
- **You cannot unstall a bump PR by closing and reopening it.** That fires a
  `reopened` event and would work in general, but not here: the API refuses with
  `422 state cannot be changed. The brew/track branch was force-pushed or
  recreated.` The only remedies for an already-parked run are the maintainer's
  approve click or `POST /actions/runs/{id}/approve`.

If the App secret is missing or rotated, the job falls back to `GITHUB_TOKEN` and
still pushes the branch — a stale `brew install` is worse than a click — but it
emits a `::warning` saying the bump will park on approval, because a PR that
silently sits at "waiting for status" reads as merely slow.

### The `brew-formula-verify` gate replaced the human eyeball (IRO-670)

A maintainer reading a generated single-file diff cannot tell a correct `sha256`
from a plausible one, so that click was latency, not review. It is now a required
status check, [`brew-formula-verify`](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/brew-formula-verify.yml),
running [`scripts/verify-homebrew-formula.sh`](https://github.com/IronSecCo/ironclaw/blob/main/scripts/verify-homebrew-formula.sh).
It fails closed on each of:

1. the diff touching anything besides `Formula/ironclaw.rb`;
2. `SHA256SUMS` for the claimed tag not cosign-verifying against
   `release.yml@refs/heads/main` and the GitHub Actions OIDC issuer — **the
   signature, not just the digest list**. A digest list nobody verified the
   signature of is not a trust anchor, it is whatever the last writer chose;
3. the re-derived formula differing from the PR head **by any byte**;
4. the generator not reporting a successful cosign verification (so the gate
   cannot silently degrade into comparing against an unsigned list); and
5. its own **non-vacuity self-test** — every run flips one hex digit of one
   `sha256` in a scratch copy and asserts the comparison rejects it. A gate only
   ever observed green is indistinguishable from `exit 0`.

Because it is a *required* check it must report on **every** PR, not just formula
ones: a required check that never reports leaves a PR stuck on "waiting for
status" forever. So it has no `paths:` filter and no job-level `if:` — it always
runs and short-circuits to a pass when the diff has no formula change.

> The job id `brew-formula-verify` **is** the required-check context recorded in
> `.github/rulesets/main.json`. Renaming the job (or giving it a `name:`) orphans
> the required check and the gate silently stops gating.

#### The base commit must be a merge base (IRO-673)

Assertion 1 above is only as good as the base it diffs against, and the first
version of this gate got that wrong: it used
`github.event.pull_request.base.sha`. **That field is not a merge base.** It is a
snapshot of the base branch *tip*, and in webhook payloads a stale one. Diffing a
PR head against it attributes every commit that landed on `main` after the PR
forked to the PR itself — and since the rolling bump touches
`Formula/ironclaw.rb` once or twice a day, a *required* check hard-failed PRs it
exists to wave through. A one-file docs PR (#569) was reported as an 84-path
formula edit and told to split itself up.

Both arms now resolve a real merge base, against
`github.event.pull_request.base.ref` rather than a hardcoded `main`:

```sh
git fetch --no-tags --quiet origin "+refs/heads/$1:refs/remotes/origin/$1"
base="$(git merge-base "refs/remotes/origin/${PR_BASE_REF}" HEAD)"
```

Two things this depends on, both easy to break by accident:

- **`fetch-depth: 0`** on the checkout. Without full history there is no merge
  base to compute.
- **No `ref:` override on the checkout.** On a `pull_request` event, checkout
  resolves GitHub's `refs/pull/N/merge`, whose first parent *is* the base commit,
  so `git merge-base` lands exactly there and the diff is precisely what the
  merge would write — stale merge ref or not. Pinning
  `ref: ${{ github.event.pull_request.head.sha }}` looks tidier and is a trap: it
  removes `main`'s copy of `scripts/verify-homebrew-formula.sh` from the
  checkout, so this required check fails every PR opened before the gate landed
  with `No such file or directory`. Same outage, different message.

The lesson generalises past this workflow: **a required check that can fail a PR
for something the PR did not do is worse than no check**, because the only
remedies on offer are relaxing the check or bypassing protection. Fix the base
resolution instead.

### Squash only. This is load-bearing, not a style preference

`allowed_merge_methods` is `["squash", "rebase"]`, but a bump PR **must** be
squash-merged:

The `formula` job writes its commit through the Contents API with an explicit
`author` (the CLA-signed maintainer, so `cla-assistant` passes — IRO-353/363).
Passing `author` makes the API set `committer` to the author too, which turns
**off** GitHub's web-flow signing. So `brew/track`'s head commit is
`verification.verified: false`, `reason: unsigned`, and main's
`required_signatures` rule rejects it. A **squash** merge discards that commit
and GitHub mints a fresh, GitHub-signed commit on main in its place. A **rebase**
merge would replay the unsigned commit verbatim and be blocked.

An earlier comment in `release.yml` claimed the Contents API signed that commit.
It does not. That wrong comment sent an operator hunting a phantom signing bug
when the real cause was the benign author pin (IRO-670).

### Trap: `gh pr merge` refuses a PR the REST API merges cleanly

`gh` reads the **precomputed** `mergeable_state`, which is stale — it still
reflects the unsigned head — so it refuses and then recommends `--admin`:

```console
$ gh pr merge 610 --squash
X ... is not mergeable: the base branch policy prohibits the merge.
  ... To use administrator privileges to immediately merge ... add the `--admin` flag.

$ gh api -X PUT repos/IronSecCo/ironclaw/pulls/610/merge -f merge_method=squash
{"sha":"51bd5bb…","merged":true,"message":"Pull Request successfully merged"}
```

**`--admin` is NOT authorized for this PR.** A plain squash satisfies every rule
in the ruleset, so `--admin` would bypass the *entire* ruleset — required checks,
signatures, linear history — to work around a stale cache. Taking the flag `gh`
suggests here trades the whole gate for a cosmetic convenience. Use the REST API
form above.

### The last click stays. Settled, not pending (IRO-670)

`pull_request.required_approving_review_count: 1` needs an approval from an actor
that is not the PR author, and the reviewer App **is** the author of the bump PR.
So one human action per release remains: dispatch `reviewer-approve.yml`, then
merge with the REST API form above.

Two ways to remove it were put to the board and **both were declined**:

- **a second long-lived credential** (another App or account to approve as) — one
  more standing credential with write reach on a protected branch, to save a
  keystroke;
- **a `bypass_actors` entry for the reviewer App** — rulesets cannot scope a
  bypass to one path, so exempting the App for `Formula/ironclaw.rb` exempts it
  for all of `main`.

The reasoning, recorded so it is not re-litigated: the 17 issues of recurring
toil were never the *click*, they were the *judgment* — a maintainer squinting at
a generated `sha256` they had no way to validate. `brew-formula-verify` removes
100% of that judgment, which was the entire win. What is left is one mechanical
action per release on the protected branch of a container-security product, and
that is a reasonable place to keep a human. **Revisit only if release cadence
makes it material** — not as a tidiness argument.

Until then, and regardless of how tempting a green-but-blocked PR makes it: do
not reach for `--admin`, and do not add a `bypass_actors` entry for the App.

### A stale tap announces itself (IRO-676, IRO-690)

Keeping the click has one cost the click itself does not name: the tap's freshness
then depends on someone *noticing* the bump PR, and for a while nothing did.
[#610](https://github.com/IronSecCo/ironclaw/pull/610) sat green and unmerged as
the third untracked bump in three days and was found by a manual sweep of open
PRs; [#614](https://github.com/IronSecCo/ironclaw/pull/614) was green and unmerged
while IRO-670 was being closed out. The README says the tap **can briefly trail**
the newest release, and "briefly" needs an enforcer.

[`brew-bump-waiting.yml`](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/brew-bump-waiting.yml)
runs on a schedule and has **two independent arms**. A red scheduled run *is* the
notification: GitHub emails on scheduled-workflow failure and it is visible in the
Actions tab.

| | Arm A — waiting bump PR | Arm B — stale tap |
| --- | --- | --- |
| Question | Is a bump blocked on a human click? | Is the tap serving an older release than the newest one? |
| Measures | open `brew/track` PRs, age from creation | `version` in `Formula/ironclaw.rb` on `main` vs `releases/latest` |
| Threshold | `THRESHOLD_MINUTES`, default 240 | `STALE_THRESHOLD_MINUTES`, defaults to arm A's |
| Clock starts | PR creation | release publication |
| Needs a PR to exist | yes | **no** |

**Arm B exists because arm A was blind to the outage it claimed to prevent
(IRO-690).** Arm A discovers work by listing open PRs, so with zero open bump PRs
it exits 0 — while its own error text claimed to protect "the tap is serving an
older release than the newest one", which it never measured. Those two come apart
precisely when no bump PR exists, and that is what IRO-689 produced: GitHub
auto-closed [#636](https://github.com/IronSecCo/ironclaw/pull/636) when its head
became `==` its base, so the tap served v0.1.447 against a published v0.1.450 with
**zero** open `brew/track` PRs. A scheduled run landing in that window would have
said "not waiting on a click" and passed. Same blindness covers every other cause
of a missing PR: a hand-closed bump PR, a `gh pr create` that fails after the
branch is pushed, a skipped formula job, or a bump that merges carrying a formula
which does not match the newest release.

Notes that matter if you touch it:

- **240 min is derived, not picked.** Real merge latency over the last 60
  `brew/track` PRs was p50 59m, p75 118m, p90 264m, max 8.2 days. The threshold
  sits above the routine path so an ordinary release never pages, and below every
  genuinely-late bump on record (#600 9h, #607 8.3h, #562 8.2d).
- **Arm A fires on age, not on "all required checks green."** Gating the alarm on
  green builds in a silent-green hole: a required check that never *reports* — the
  IRO-670 and IRO-673 failure mode, twice — reads as "not green", so the alarm
  would go quiet on exactly the PRs that are most stuck. Check state is reported
  in the error message as the remediation hint instead.
- **Arm B reads `main` over the API, never the local checkout.** The guard also
  runs on `test/brew-bump-waiting-*` branches, and measuring whatever ref happens
  to be checked out would report that branch's formula as "what users get". This
  is also why a control-branch push cannot drive arm B red.
- **Arm B has no grace window when the formula is *ahead* of `releases/latest`.**
  Behind is ordinary staleness and gets the threshold. Ahead means the formula's
  download URLs point at a release that was deleted, unpublished, or demoted to a
  prerelease — that does not heal with time, so waiting out a threshold would only
  delay the alarm.
- **A trail inside the threshold is not a bug.** The README advertises that the tap
  **can briefly trail**; arm B bounds that window rather than forbidding it. The
  IRO-689 window as observed was ~40 min, which arm B would also have passed, and
  correctly. What arm B changes is that the window can no longer persist
  *unbounded and silently*.
- **Scheduled-failure email goes to whoever last edited the `cron:` line**, not to
  the repo's watchers. If a bot identity ever rewrites that line the alarm
  silently redirects to an unread inbox. Keep it a human who can do the merge.
- **It is notify-only and must stay that way.** Read-only scopes, no merge, no
  approve, and deliberately **not** in `required_status_checks` — it gates
  nothing, and requiring it would block unrelated PRs whenever the tap trailed.
  Arm B added no new scope: `releases/latest` and the formula both read under the
  `contents: read` the checkout already needed.
- **To re-prove it** (a guard that has never gone red is not evidence that it can),
  the two arms have different controls. Arm A: push a branch named
  `test/brew-bump-waiting-<minutes>`, which runs against the live API with that
  threshold, so a small number reports the currently-open bump PR and a large one
  reports it as within the window. Arm B: `scripts/tests/test_check_brew_bump_waiting.py`
  drives the script through a stubbed `gh` with the real IRO-689 pair (formula
  0.1.447, release v0.1.450) and asserts non-zero, with the fresh pair asserted
  green on the same code path. The pre-IRO-690 script is recorded green on that same
  fixture, which is what makes the red assertion evidence rather than decoration.

### What the cron actually delivers (IRO-679)

This guard, and the fork-CI approval backstop next to it, are both `schedule:`
crons. A cron promise is worth what the scheduler delivers, and an earlier
version of this page put detection latency at **"≤ 60 min"** on the strength of
the cron expression alone. That was not measured, and it is not true.

[`scripts/measure-cron-latency.py`](https://github.com/IronSecCo/ironclaw/blob/main/scripts/measure-cron-latency.py)
measures it. Run it before writing any latency number here. Re-measured
2026-07-30 **11:47Z**, over **57 intervals** of this repo's daily and weekly crons:

| quantity | measured |
|---|---|
| gap delivered vs period requested (daily+weekly) | p50 **-8 min**, p90 **+36**, p95 **+51**, max **+75** |
| intervals overshooting nominal by >60 min | **2 / 57** |
| declared clock time vs actual fire time | daily/weekly crons fire **~3h late**, consistently |

Two things follow, and they pull in opposite directions:

- **Cadence is honoured at daily and weekly frequency.** A daily cron runs daily
  to within about an hour. It does **not** follow that an hourly cron runs hourly
  — see below.
- **Phase is not honoured.** Every daily/weekly cron here fires about three
  hours after its declared UTC time. Cadence survives this; a sentence of the
  form *"runs at 03:17 UTC"* does not. Do not write one.

Caveats, because this is the part that is easy to overstate:

- **Hourly is in the dropped bucket. There is no latency bound to quote.** This
  used to read "the hourly cadence is unmeasured", with a projected worst case of
  ~135 min (60 nominal + the 75 min daily/weekly overshoot) and ~105 min of
  headroom against the 240 min threshold. The hourly guard was made the experiment
  that settles it, and it has now returned data that kills the projection:

  | cron | period asked | delivered gap | excess | live silence |
  |---|---|---|---|---|
  | `brew-bump-waiting` `23 * * * *` | 60 min | **181 min** | **+121** | 122 min = **2.0x**, nothing queued |
  | `fork-ci-approval-backstop` `13,43 * * * *` | 30 min | 143 min | **+113** | 104 min = **3.5x**, nothing queued |

  GitHub delivered **2 of the 7 slots due** to the brew guard and dropped five.
  Both sub-hourly crons in the repo now exceed their own period, independently,
  which matches GitHub's documented deprioritisation of high-frequency schedules.
  One interval is not a distribution, so do not turn 181 min into a bound either —
  it is a floor on what has been observed, not a ceiling. IRO-680 re-runs the
  measurement once there are more intervals.
- **The 240 min threshold survives, with less headroom than claimed.** 181 min
  against 240 leaves ~59 min, not ~105. The design still holds for a reason
  independent of the numbers: **both arms fire on a level, not an edge.** An open
  bump PR stays open and a stale formula stays stale, so a dropped run delays
  detection and never loses it. Only time-to-notice degrades.
- **Quote the silence, not the gap count.** Two intervals cannot carry that
  conclusion on their own. What carries it is the script's second table: **live
  silence**, the still-open interval since the last scheduled run. Re-measured
  2026-07-30 06:08Z, the backstop had been silent **236 min against a 30 min
  period (7.9x)** while `state: active` with nothing queued — and across the
  7h40m since its first-ever scheduled run, a 30-min cron predicts ~15 runs
  where the scheduler delivered **3**. That is one direct observation with no
  sample-size caveat attached. In the same table every daily/weekly cron in the
  repo sat at **0.0x–0.9x** of its period, so the metric is not merely flagging
  everything it looks at.
- **The `13,43` offset did not help.** It was a free experiment — same cadence,
  off the two most congested minutes of the hour. It now has a post-offset sample
  and the answer is no: under `13,43`, the backstop delivered 2 runs with a 143 min
  gap against a 30 min period, and sat at 3.5x live silence. Keep the offset (it
  costs nothing and n=1 gap is thin), but stop treating minute-of-hour as the lever.
  The brew guard's own `23 * * * *` is off-peak too and is dropped just as hard, so
  the deprioritisation is about frequency, not about which minute you ask for.
- **A cron is the wrong tool for a hard latency bound.** Getting a real one
  needs an event-driven trigger, and for the approval backstop that is a
  trust-model change, not a scheduling tweak: the safety net must stay
  `schedule`/`workflow_dispatch`-only so contributor-controlled content cannot
  influence the thing that approves contributor-controlled content. Latency is
  not a reason to trade that away.

## Branch protection: the reviewer path needs no change

The machine-reviewer path above needs **no edit** to
[`.github/rulesets/main.json`](https://github.com/IronSecCo/ironclaw/blob/main/.github/rulesets/main.json).
The App's approving review satisfies the existing
`pull_request.required_approving_review_count: 1`. The admin `bypass_actors`
entry stays as the break-glass path of last resort, but with a working machine
reviewer it should no longer be the *routine* way security PRs merge.

The one ruleset change made since is a **ratchet up**, not a relaxation:
`brew-formula-verify` was added to `required_status_checks` (IRO-670) so the
formula gate actually gates. Protection on `main` only ever moves in that
direction — if a required check is wrong, fix the check on its own ticket rather
than removing it to unblock a merge.

If we ever adopt Option B instead, the only ruleset change would be flipping
`require_code_owner_review` to `true` after the reviewer account/team is a
CODEOWNER — tracked in [CODEOWNERS](https://github.com/IronSecCo/ironclaw/blob/main/.github/CODEOWNERS).

## Human handoff (one-time setup)

Everything an agent can do is already in this repo (manifest, workflow,
CODEOWNERS, docs). The **only** step that genuinely needs a human is creating and
installing the App and storing its credentials — GitHub App creation requires an
interactive browser session and yields a private key an agent must never handle.
**Escalated to the CEO.**

Click-by-click (≈5 minutes, repo admin):

1. **Create the App.** Go to
   `https://github.com/organizations/IronSecCo/settings/apps/new`.
   - **GitHub App name:** `ironclaw-reviewer`
   - **Homepage URL:** `https://github.com/IronSecCo/ironclaw`
   - **Webhook:** uncheck **Active** (no webhook needed).
   - **Repository permissions:** **Pull requests → Read and write**,
     **Contents → Read and write**, **Metadata → Read-only** (mandatory). Leave
     everything else **No access**. (Contents **write** is required: GitHub only
     counts an approving review toward required approvals if the reviewer has
     write access — with read-only the App's approval is recorded but not
     counted. `main` stays PR+checks protected, so this only makes the approval
     count, it does not let the App bypass review.)
   - **Where can this App be installed?** *Only on this account.*
   - Click **Create GitHub App**.
   *(The values above match [`.github/reviewer-app-manifest.yml`](https://github.com/IronSecCo/ironclaw/blob/main/.github/reviewer-app-manifest.yml).)*
2. **Note the Client ID** shown on the App's settings page (the `Iv23...` value, not
   the numeric App ID — `actions/create-github-app-token` deprecated the `app-id`
   input in favour of `client-id`). It is also readable with
   `gh api /apps/ironclaw-reviewer --jq .client_id`.
3. **Generate a private key:** on the App page → **Private keys** →
   **Generate a private key**. A `.pem` downloads. Treat it as a secret.
4. **Install the App:** App page → **Install App** → install on **IronSecCo**,
   scoped to **only the `ironclaw` repository**.
5. **Store the Client ID as a repo variable and the key as a repo secret**
   (Settings → Secrets and variables → Actions), or via CLI:
   ```bash
   gh variable set REVIEWER_APP_CLIENT_ID --repo IronSecCo/ironclaw --body "<the Client ID>"
   gh secret set REVIEWER_APP_PRIVATE_KEY --repo IronSecCo/ironclaw < path/to/ironclaw-reviewer.*.private-key.pem
   ```
   Then delete the local `.pem`. The Client ID is a variable, not a secret: it is a
   public identifier that grants nothing without the private key, and keeping it
   readable makes a failed mint diagnosable from the run log.
6. *(Optional, only if adopting Option B / CODEOWNERS enforcement)* create the
   `@IronSecCo/reviewers` team and add the reviewer as a member so the
   [CODEOWNERS](https://github.com/IronSecCo/ironclaw/blob/main/.github/CODEOWNERS)
   owner resolves.

### Verification (run once the App exists)

This proves the acceptance criterion — *a non-author reviewer approval satisfies
the `main` required-review gate*:

1. Open a throwaway PR against `main` (e.g. a no-op docs edit) as `omerzamir`.
2. Let `build` + `CodeQL` go green.
3. Run the reviewer workflow:
   ```bash
   gh workflow run reviewer-approve.yml -f pr=<PR_NUMBER> -f review_of_record="verification: IRO-148"
   ```
4. Confirm `ironclaw-reviewer[bot]` posted an **Approved** review and that the PR
   page shows **"1 approving review"** with the required-review check satisfied
   and the merge button enabled **without** admin bypass.
5. Close the throwaway PR.

Record the result on
[IRO-148](https://github.com/IronSecCo/ironclaw) and remove the admin-bypass
reliance for security PRs going forward.
