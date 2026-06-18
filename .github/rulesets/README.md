# Branch rulesets — source of truth

[`main.json`](main.json) is the **source of truth** for the `main`-branch protection
ruleset. Keep the repository's live ruleset in sync with this file: review changes
here and re-apply.

## Shape

External contributions arrive via pull request and must satisfy every rule below.
Repository **admins are listed as `bypass_actors`** so maintainers can land and revert
quickly when needed.

## Rules enforced

| Rule | Effect |
|---|---|
| `deletion` | the `main` branch cannot be deleted |
| `non_fast_forward` | no force-pushes / history rewrites |
| `required_linear_history` | merge commits are rejected (linear history) |
| `required_signatures` | commits must be signed |
| `required_status_checks` | `build` (CI) and `CodeQL` must be green |

## Applying

```sh
# First time (create):
gh api -X POST /repos/nivardsec/ironclaw/rulesets --input .github/rulesets/main.json

# Update an existing ruleset (look up its id first):
gh api /repos/nivardsec/ironclaw/rulesets --jq '.[] | "\(.id) \(.name)"'
gh api -X PUT /repos/nivardsec/ironclaw/rulesets/<id> --input .github/rulesets/main.json
```
