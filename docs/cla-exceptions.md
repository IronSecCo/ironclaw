---
title: "CLA exceptions"
description: "The record of every case where IronClaw accepted a contribution without a captured CLA signature, why, and the evidence relied on."
---

# CLA exceptions

IronClaw requires a signed Contributor License Agreement before a contribution is
merged. The check is `license/cla`, served by the hosted
[cla-assistant.io](https://cla-assistant.io) app.

This page is the complete record of every case where we merged, or authorised
merging, without that check going green. It exists because an exception that is
not written down is indistinguishable from a process failure. Each entry states
what evidence we relied on instead, who authorised it, and what its scope was.

An entry here is a record of a **defect in our tooling**, not of a contributor
cutting a corner. Where the contributor's assent is on the public record and only
our automation failed to capture it, we say so plainly.

## Standing rules

These constrain every entry on this page.

- **`license/cla` is a policy gate, not a technical one.** It is deliberately not
  one of the required status checks in the `main-protection` ruleset (those are
  exactly `build` and `CodeQL`). Honouring it is a choice we make. An exception is
  therefore recorded here in prose; it never involves editing a ruleset, relaxing
  a required check, or bypassing branch protection.
- **A real signature always beats an exception.** Before acting on any exception,
  re-check the live status. A late signature retires the exception rather than
  running alongside it.
- **No entry is a precedent and no entry is a waiver.** An exception applies only
  to the specific pull requests it names. It does not waive the CLA for the named
  contributor's future contributions, does not create a standing rule, and does
  not license anyone to merge on inferred assent in a case not listed here.
- **The licence grant still has to exist.** An exception is only available when
  the contributor has unambiguously and publicly assented to the CLA text. It is
  never available to skip assent, only to accept assent our tooling failed to
  capture.

## Exception 001: syf2211, pull requests #568, #569, #574

**Status:** authorised, bounded, and as of 2026-07-30 **not yet exercised**. No
pull request has been merged under this exception.

**Scope:** exactly three pull requests, and no others:

| PR | Title | Head commit |
| --- | --- | --- |
| [#568](https://github.com/IronSecCo/ironclaw/pull/568) | `feat(ironctl): add --json output mode to tools command` | `fcad8db` |
| [#569](https://github.com/IronSecCo/ironclaw/pull/569) | `docs(scan): link hardening guides hub from scan reference` | `7bc7820` |
| [#574](https://github.com/IronSecCo/ironclaw/pull/574) | `test(scan): add named subtests for grade bands and isolation runtimes` | `4c79c8b` |

### What happened

The contributor was told, by us, to sign the CLA by replying to the check comment
with the CLA phrase. **That instruction was wrong.** cla-assistant ships no
`issue_comment` handler, so a comment can never register a signature; the only
route that works is the one-click link on the check itself.

Acting on our incorrect instruction, the contributor posted the exact CLA phrase
five times across the three pull requests, between 24 and 27 July 2026, each time
followed by a `recheck` comment. Every one of those attempts was destined to fail
for a reason they had no way to see and no way to fix.

The correction, with the working one-click link, did not reach the threads until
2026-07-29 (14:20Z on #568 and #574, 19:25Z on #569).

The licence grant we need is therefore already on the public record, in the
contributor's own words, five times over. What is missing is only the hosted
service's record of it, and that gap is **our defect**: we gave the wrong
instruction, and we left it uncorrected for several days while they burned
attempts on it.

### Evidence relied on

Each link below is a public comment authored by `syf2211` whose body is exactly
the CLA assent phrase, `I have read the CLA Document and I hereby sign the CLA`.

On [#568](https://github.com/IronSecCo/ironclaw/pull/568):

1. [`issuecomment-5066684805`](https://github.com/IronSecCo/ironclaw/pull/568#issuecomment-5066684805), 2026-07-24 06:07:06Z
2. [`issuecomment-5072953756`](https://github.com/IronSecCo/ironclaw/pull/568#issuecomment-5072953756), 2026-07-24 18:03:43Z
3. [`issuecomment-5078420753`](https://github.com/IronSecCo/ironclaw/pull/568#issuecomment-5078420753), 2026-07-25 12:06:03Z

On [#569](https://github.com/IronSecCo/ironclaw/pull/569):

4. [`issuecomment-5083382851`](https://github.com/IronSecCo/ironclaw/pull/569#issuecomment-5083382851), 2026-07-26 12:03:30Z

On [#574](https://github.com/IronSecCo/ironclaw/pull/574):

5. [`issuecomment-5086100535`](https://github.com/IronSecCo/ironclaw/pull/574#issuecomment-5086100535), 2026-07-27 00:17:18Z

**How this list was verified.** On 2026-07-30 every comment on all three pull
requests was enumerated through the GitHub API and each body compared against the
CLA phrase character for character. Five comments match exactly. The pull request
review and review-comment endpoints hold no comments at all on these three pull
requests, so nothing is hiding there. The issue timeline reports 11, 7 and 8
comment events on #568, #569 and #574 against 11, 7 and 8 comments returned by
the comments endpoint, so no matching comment has been deleted. Five is the
complete count, not a sample.

### Authorisation and preconditions

Authorised by the CEO as a bounded exception. It may be exercised only when all of
the following hold.

1. **Not before 2026-07-31 24:00Z.** The contributor was given until 2026-08-01 to
   click the link, and that time is theirs. Merging on the exception before then
   would take a deadline away from someone who is still inside it.
2. **`license/cla` re-checked live and still not green**, read from
   `GET /repos/IronSecCo/ironclaw/commits/{head_sha}/status`. If it has gone
   green, this exception is void and unnecessary: merge on the real signature.
3. **CI green on the head commit** under the ruleset's required checks, `build`
   and `CodeQL`. The exception covers the CLA policy gate and nothing else.
4. **Merged by squash.** Required, not stylistic: `main` enforces
   `required_signatures`, and the contributor's own commits are `verified: false`
   (`unknown_key`, their signing key is not registered with GitHub). A squash
   merge is re-signed by GitHub and satisfies the rule. A rebase merge would
   replay the unverified commits and be rejected, and a merge commit is barred by
   `required_linear_history`.

### What this entry does not do

It does not waive the CLA for `syf2211`. Their next contribution needs a signature
like anyone else's, and the one-click link now works and has been given to them.
It does not establish that public comments substitute for signatures in general;
they do not, and the tooling cannot read them. It applies to the three pull
requests named above and to nothing else.

### The durable fix, already landed

The instruction that caused this was wrong in our own words on our own threads, so
the fix belongs in the contributor path rather than in anyone's memory.
[CONTRIBUTING](https://github.com/IronSecCo/ironclaw/blob/main/CONTRIBUTING.md) now
leads with the one-click link and states outright that signing by comment does not
work here, naming the reason: the bot reads pull request events only, so a sign
comment is silently ignored and `license/cla` stays red no matter how many times it
is posted. That is the guidance that should have been on these threads from the
start.
