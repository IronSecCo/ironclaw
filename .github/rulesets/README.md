# Branch rulesets — source of truth

[`main.json`](main.json) is the **source of truth** for the `main`-branch protection
ruleset. Keep the repository's live ruleset in sync with this file: review changes
here and re-apply.

## Shape

Every change to `main` arrives via pull request and must satisfy every rule below.

`bypass_actors` is **empty, on purpose**. It used to list the repository admin role
with `bypass_mode: always`, which meant the required review was advisory for every
maintainer and mandatory for everyone else; two commits reached `main` unreviewed
that way before it was removed (IRO-731). The approving review is now satisfied by
the reviewer App instead, which needs no bypass. See
[`docs/merge-exceptions.md`](../../docs/merge-exceptions.md) for the record and for
the authorised procedure to put the bypass back if it is ever genuinely needed.

## Rules enforced

| Rule | Effect |
|---|---|
| `deletion` | the `main` branch cannot be deleted |
| `non_fast_forward` | no force-pushes / history rewrites |
| `required_linear_history` | merge commits are rejected (linear history) |
| `required_signatures` | commits must be signed |
| `pull_request` | one approving review from a non-author; stale reviews are dismissed on push; squash or rebase only |
| `required_status_checks` | `build` (CI), `CodeQL` and `brew-formula-verify` must be green |

## Applying

```sh
# First time (create):
gh api -X POST /repos/IronSecCo/ironclaw/rulesets --input .github/rulesets/main.json

# Update an existing ruleset (look up its id first):
gh api /repos/IronSecCo/ironclaw/rulesets --jq '.[] | "\(.id) \(.name)"'
gh api -X PUT /repos/IronSecCo/ironclaw/rulesets/<id> --input .github/rulesets/main.json
```
