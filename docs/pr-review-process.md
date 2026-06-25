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
  (SHA-pinned). The App private key is read from `secrets.REVIEWER_APP_PRIVATE_KEY`
  and masked by the action — it is never logged.
- **Safety:** the workflow refuses to approve if the App secret is absent, if
  the PR is not open, or if the PR author is the App itself.

Until the App is installed and the two secrets exist, the workflow is **inert**
(a manual dispatch fails fast with a clear message). Nothing in CI or the
release path depends on it, so landing the scaffold changes no current behaviour.

## Branch protection: no change required

The recommended path needs **no edit** to
[`.github/rulesets/main.json`](https://github.com/IronSecCo/ironclaw/blob/main/.github/rulesets/main.json).
The App's approving review satisfies the existing
`pull_request.required_approving_review_count: 1`. The admin `bypass_actors`
entry stays as the break-glass path of last resort, but with a working machine
reviewer it should no longer be the *routine* way security PRs merge.

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
2. **Note the App ID** shown on the App's settings page.
3. **Generate a private key:** on the App page → **Private keys** →
   **Generate a private key**. A `.pem` downloads. Treat it as a secret.
4. **Install the App:** App page → **Install App** → install on **IronSecCo**,
   scoped to **only the `ironclaw` repository**.
5. **Store the credentials as repo secrets** (Settings → Secrets and variables →
   Actions → New repository secret), or via CLI:
   ```bash
   gh secret set REVIEWER_APP_ID --repo IronSecCo/ironclaw --body "<the App ID>"
   gh secret set REVIEWER_APP_PRIVATE_KEY --repo IronSecCo/ironclaw < path/to/ironclaw-reviewer.*.private-key.pem
   ```
   Then delete the local `.pem`.
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
