---
title: "Merge exceptions"
description: "The record of every commit that reached IronClaw's main branch without the approving review the branch ruleset requires, and what we changed so it cannot happen again."
---

# Merge exceptions

`main` is protected by the `main-protection` branch ruleset, checked in as
[`.github/rulesets/main.json`](https://github.com/IronSecCo/ironclaw/blob/main/.github/rulesets/main.json).
Among other rules it requires **one approving review** from an actor other than
the pull request author before anything lands.

This page is the complete record of every commit that reached `main` **without
that review**. It exists for the same reason [CLA exceptions](cla-exceptions.md)
does: an exception that is not written down is indistinguishable from a process
failure.

The entries below are not exceptions in the sense that page uses. Nobody
authorised them in advance and nothing was weighed against a rule before the
merge happened. They were **process failures on our side**. The diffs turned out
to be benign, and each was read line by line afterwards, but *afterwards* is the
problem: the check that was supposed to run before the push ran after it, by
hand, because someone noticed.

Blame belongs to the mechanism, not to whoever typed the command. Until
2026-08-01 the ruleset carried a single bypass actor, the repository **admin**
role, with `bypass_mode: always`. Every IronClaw agent drives GitHub as
`omerzamir`, who holds that role, so the required review was **advisory for us
and mandatory for everyone else**. Worse, `gh pr merge --squash` reads a stale
`mergeable_state` and helpfully suggests `--admin` when it does; the tool
recommended the bypass at exactly the moment someone was already stuck. Both
entries below are that shape.

## Standing rules

These constrain every entry on this page.

- **Every bypassed merge gets an entry, without exception and without delay.**
  Content being harmless is not a reason to leave it unrecorded. It is a reason
  the entry is short.
- **An entry is not an authorisation.** Nothing here grants permission to repeat
  it, and no entry is a precedent for the next merge that feels urgent.
- **The record is derived from GitHub, not from memory.** Each entry cites the
  rule-suite evaluation id that GitHub itself recorded, so the claim can be
  re-checked independently of this page.
- **If you are reaching for `--admin`, stop.** The
  [restore procedure](#restore-procedure-break-glass) below is the supported way
  out, and it is deliberately slower and deliberately logged.

## How this record was assembled

Bypasses are read from the rule-suite history, which is GitHub's own log of
every push evaluated against the ruleset:

```sh
gh api "repos/IronSecCo/ironclaw/rulesets/rule-suites?ref=refs/heads/main&per_page=100"
gh api "repos/IronSecCo/ironclaw/rulesets/rule-suites/<id>"
```

A row with `"result": "bypass"` is a push that a rule would have rejected and an
actor was permitted to skip. The per-suite endpoint names the rule that failed.

**Coverage limit, stated plainly.** Queried on 2026-08-01, that history returned
20 evaluations for `refs/heads/main`, the oldest pushed 2026-07-31T04:41:43Z.
GitHub does not retain rule-suite rows indefinitely, so this page is complete for
the window the API still serves and **cannot** speak for anything older. Two of
those 20 rows were bypasses. The other 18 passed cleanly.

## Entry 001: `c63ac4e3`, pull request #660

**Merged** 2026-07-31T12:39:43Z by `omerzamir`. Rule-suite evaluation
`3517044004`, advancing `main` from `85c2b769` to `c63ac4e3`.

[#660](https://github.com/IronSecCo/ironclaw/pull/660),
`ci(docs): turn on the bare-opening-fence rule, held back since IRO-707 [IRO-710]`,
head branch `ci/iro710-bare-fence-rule`. Five files:

| File | Kind |
| --- | --- |
| `scripts/check-md-fences.py` | CI enforcement script |
| `scripts/tests/test_check_md_fences.py` | test for that script |
| `docs/assets/live-containment-social/STORYBOARD.md` | docs |
| `docs/blog/scan-a-dockerfile-for-security-issues.md` | docs |
| `docs/site/quickstart.mdx` | docs |

**Reviews at merge: none.** `reviewDecision` was `REVIEW_REQUIRED` and the review
list was empty. GitHub recorded the `pull_request` rule as `fail` with the reason
*"At least 1 approving review is required by reviewers with write access"*. Every
other rule in the suite passed: `required_signatures`, `required_status_checks`,
`required_linear_history`, `non_fast_forward`, `deletion`. So this was not a
broken CI run being forced through. It was the review, and only the review, that
was skipped.

**Content verified after the fact, not before.** The diff turns on a lint rule
for unlabelled Markdown code fences and fixes the three files that violated it.
It is benign. But note what it is: a **change to a CI enforcement script**. The
comfortable reading of a bypass, that it was only a harmless cleanup, is weaker
here than anywhere. A defect in that file weakens a gate for every pull request
that follows, which is precisely the category that most needs a second reader.

## Entry 002: `f669faa8`, pull request #673

**Merged** 2026-07-31T20:40:03Z by `omerzamir`, eight hours after entry 001.
Rule-suite evaluation `3522187994`, advancing `main` from `8b23ae67` to
`f669faa8`.

[#673](https://github.com/IronSecCo/ironclaw/pull/673),
`scan: name the flag the user typed, and make the image-mode gate defensible [IRO-724]`,
head branch `fix/iro724-scan-review-nits`. Three files: `cmd/ironctl/scan.go`,
`cmd/ironctl/scan_flags_test.go`, `cmd/ironctl/scan_test.go`.

**Reviews at merge: none.** Identical shape to entry 001: `reviewDecision` was
`REVIEW_REQUIRED`, the review list was empty, and the recorded rule failure was
again the approving-review requirement with every other rule passing.

**Content verified after the fact, not before.** The diff improves `ironctl scan`
error messages and tightens a mode gate, with tests. It is benign and it is
covered by the required `build` check that did run. What it was not is *read by
anyone other than its author before it reached `main`*.

**Two in eleven hours is a pattern, not a slip.** This is why the fix below is
mechanical rather than a reminder to be careful. The first bypass was invisible
until the second one was investigated, and the second was found only because
someone went looking at the first.

## What changed on 2026-08-01

The admin bypass actor was **removed** from ruleset `17917971`. `bypass_actors`
is now `[]`. Every rule is otherwise untouched, including the required review
count and the required status check list.

No legitimate flow depended on it. The
[machine reviewer of record](pr-review-process.md) satisfies
`required_approving_review_count` without any bypass, and that was demonstrated
on live traffic before the actor was removed: pull request #668 received a real
`ironclaw-reviewer` **APPROVED** review, its merge state went `CLEAN`, and it
squash-merged through the plain REST endpoint with no admin flag anywhere.

**The intended consequence.** A required check that is stuck, or missing
entirely, now blocks every merge to `main` until it clears. That is not a
side effect to be worked around; it is the point. A gate that any of us can step
over on a bad night is a gate that records mistakes instead of preventing them.

## Restore procedure (break glass)

If the bypass genuinely has to come back, this is how, and it is written down
here so that the next person under pressure finds a procedure instead of
reaching for `--admin`.

This is a **deliberate, logged act**, not a flag on a merge command. It needs CEO
authorisation, and using it means writing a new entry on this page.

Read the ruleset first and keep the response. `PUT` **replaces** the whole
object, so a payload carrying only `bypass_actors` silently deletes every rule
protecting `main`:

```sh
gh api repos/IronSecCo/ironclaw/rulesets/17917971 > before.json
```

Build the payload from that file. Keep `name`, `target`, `enforcement`,
`conditions` and the **entire** `rules` array byte for byte; change only
`bypass_actors`:

```sh
python3 - <<'PY' > payload.json
import json
d = json.load(open("before.json"))
json.dump(
    {
        "name": d["name"],
        "target": d["target"],
        "enforcement": d["enforcement"],
        "conditions": d["conditions"],
        "rules": d["rules"],
        "bypass_actors": [
            {"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}
        ],
    },
    open("payload.json", "w"),
)
PY
gh api -X PUT repos/IronSecCo/ironclaw/rulesets/17917971 --input payload.json
```

Then re-read the ruleset and diff it against `before.json`. The only difference
must be `bypass_actors`:

```sh
gh api repos/IronSecCo/ironclaw/rulesets/17917971 > after.json
diff <(jq -S 'del(.updated_at)' before.json) <(jq -S 'del(.updated_at)' after.json)
```

Finally, update
[`.github/rulesets/main.json`](https://github.com/IronSecCo/ironclaw/blob/main/.github/rulesets/main.json)
in the same change. It is the checked-in source of truth and the README beside it
tells people to re-apply it with `PUT`, so a live ruleset that disagrees with
that file will be quietly overwritten by the next person who follows those
instructions.
