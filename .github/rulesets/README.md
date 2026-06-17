# Branch rulesets — source of truth

[`main.json`](main.json) is the **source of truth** for the `main`-branch
protection ruleset. The repository's live ruleset should be kept in sync with
this file; review changes here and re-apply.

## Why this shape

IronClaw lands changes by **authorized direct push to `main`** — two maintainer
agents rebase on `origin/main`, run the CGO preflight, and push non-force (see
[`AGENTS.md`](../../AGENTS.md)). A conventional pull-request review gate would
break that flow, so this ruleset deliberately:

- **omits any `pull_request` rule** (no required reviews), and
- lists repo **admins as `bypass_actors`** (`RepositoryRole` 5, `always`), so the
  authorized maintainers pushing directly are not blocked by the required status
  checks.

External contributors (non-admins) are still fully protected: their changes must
arrive via PR and satisfy every rule below.

## Rules enforced

| Rule | Effect |
|---|---|
| `deletion` | the `main` branch cannot be deleted |
| `non_fast_forward` | no force-pushes / history rewrites |
| `required_linear_history` | merge commits are rejected (linear history) |
| `required_signatures` | commits must be signed |
| `required_status_checks` | `build` (CI) and `CodeQL` must be green |

> The `CodeQL` check becomes effective once the CodeQL workflow lands (T-254).
> Until then it applies only to non-bypass actors via PR; admins bypass.

## Applying

```sh
# First time (create):
gh api -X POST /repos/nivardsec/ironclaw/rulesets --input .github/rulesets/main.json

# Update an existing ruleset (look up its id first):
gh api /repos/nivardsec/ironclaw/rulesets --jq '.[] | "\(.id) \(.name)"'
gh api -X PUT /repos/nivardsec/ironclaw/rulesets/<id> --input .github/rulesets/main.json
```
