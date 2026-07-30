#!/usr/bin/env bash
# Freshness guard for the Homebrew tap (IRO-676).
#
#   scripts/check-brew-bump-waiting.sh
#
# Run from a schedule (.github/workflows/brew-bump-waiting.yml) so a stale tap
# ANNOUNCES ITSELF instead of waiting to be found by someone sweeping open PRs by hand.
#
# TWO ARMS, TWO FAILURE MODES (arm B added by IRO-690)
# ---------------------------------------------------
#   A. "Is a bump waiting on a human?" — an open `brew/track` PR older than
#      THRESHOLD_MINUTES.
#   B. "Is the tap actually stale?" — the `version` in Formula/ironclaw.rb on
#      TRACK_BRANCH (what `brew install` serves) against `releases/latest` (what it
#      should serve), red once the newest release has been published longer than
#      STALE_THRESHOLD_MINUTES.
#
# Arm A alone was the whole guard until IRO-690, and it is blind to the exact outage
# IRO-689 produced. It discovers work by LISTING OPEN PRs, so with zero open bump PRs
# it exits 0 — while claiming in its own error text to protect a property it never
# measured ("the tap is serving an older release than the newest one"). Those two come
# apart precisely when no bump PR exists, which is what IRO-689 did: GitHub auto-closed
# #636 when its head became == its base, so the tap served v0.1.447 against a published
# v0.1.450 with zero open `brew/track` PRs. Arm A would have said "not waiting on a
# click" and passed. Same blindness covers every other missing-PR cause: a hand-closed
# bump PR, a `gh pr create` that fails after the branch is pushed, a skipped formula
# job, or a bump that merges carrying a formula which does not match the newest release.
#
# Arm B measures the user-visible property directly and needs no PR to exist, so it is
# non-vacuous by construction. Arm A is kept: it catches a different thing (a bump that
# IS trying to land but is blocked on a human click), and it catches it before the tap
# has gone stale enough for arm B to fire.
#
# Honest scope: arm B would NOT have fired during IRO-689's observed window either,
# because that window was ~40 min and the threshold is 240 — a 40-minute trail is
# inside the README's advertised "can briefly trail" budget, so green there is correct.
# What arm B changes is that the window can no longer persist UNBOUNDED and silently.
#
# Why this exists
# ---------------
# IRO-670 removed the per-release human judgement on four sha256 values (the required
# `brew-formula-verify` check re-derives the formula from a cosign-verified
# SHA256SUMS), but the board settled that the approving-review click stays. So tap
# freshness now depends entirely on someone NOTICING the bump PR, and nothing
# announced one. #610 sat green and unmerged and was the third untracked bump in
# three days; #614 was green and unmerged while IRO-670 was being closed out. The tap
# once carried the newest release for six minutes.
#
# README says the tap "can briefly trail" the newest release. This is the thing that
# makes "briefly" true rather than aspirational.
#
# Arm A fire condition: AGE, not check state
# ------------------------------------------
# This goes red on any open bump PR older than the threshold, whatever its checks
# say, and reports the required-check state in the message as a remediation hint
# (all green => only the click is missing; not green => CI is the blocker, fix that
# first). Gating the alarm on "all required checks green" would have been the natural
# reading, but it builds in a silent-green hole: a required check that never REPORTS
# (the IRO-673 / IRO-670 failure mode, twice now) would mean "not all green", so the
# alarm would stay quiet on exactly the PRs that are most stuck. A stale tap is a
# stale tap either way.
#
# Age is measured from PR creation, deliberately. If the branch is ever force-pushed
# onto a reused PR, that only makes the measured age LARGER than the newest bump's
# real wait, so the guard over-fires rather than under-fires. Wrong in the loud
# direction is the only acceptable direction here.
#
# Read-only. No merge, no approve, no write scope of any kind. It notifies; it does
# not repair, and it is deliberately NOT a required status check (it gates nothing,
# and adding it to required_status_checks would block unrelated PRs).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Overridable so the threshold can be driven from a workflow_dispatch input: tuning it
# needs to be possible, and proving the red path needs a value smaller than a real
# wait. Default 240 (4h) is derived from the actual merge latency of the last 60
# `brew/track` PRs: p50 59m, p75 118m, p90 264m, max 8.2 DAYS. 4h sits above the
# routine path (so an ordinary release never pages) and below every genuinely-late
# bump on record (#600 9h, #607 8.3h, #562 8.2d).
THRESHOLD_MINUTES="${THRESHOLD_MINUTES:-240}"
REPO="${REPO:-IronSecCo/ironclaw}"
HEAD_BRANCH="${HEAD_BRANCH:-brew/track}"
RULESET="${ROOT}/.github/rulesets/main.json"

# Arm B's clock is a DIFFERENT quantity from arm A's (release publication -> tap in
# sync, versus bump-PR open -> merged), so it gets its own knob. It defaults to arm A's
# value rather than a second hardcoded number: the two windows differ by only the few
# minutes between the release publishing and its bump PR opening, and one number that
# cannot drift beats two that can.
STALE_THRESHOLD_MINUTES="${STALE_THRESHOLD_MINUTES:-$THRESHOLD_MINUTES}"

# The branch `brew tap` actually serves. Read over the API rather than from the local
# checkout on purpose: this script also runs on `test/brew-bump-waiting-*` control
# branches, and measuring whatever ref happens to be checked out would report that
# branch's formula as "what users get". Pinning the ref keeps arm B measuring the tap.
TRACK_BRANCH="${TRACK_BRANCH:-main}"
FORMULA_PATH="${FORMULA_PATH:-Formula/ironclaw.rb}"

say()  { printf '==> %s\n' "$*"; }
fail() { printf '::error::%s\n' "$*" >&2; exit 1; }

require_positive_minutes() {
  local name="$1" value="$2"
  case "$value" in
    ''|*[!0-9]*) fail "${name} must be a whole number of minutes, got '${value}'." ;;
  esac
  [ "$value" -gt 0 ] || fail "${name} must be > 0; a zero threshold pages on every release the instant it is cut."
}
require_positive_minutes THRESHOLD_MINUTES "$THRESHOLD_MINUTES"
require_positive_minutes STALE_THRESHOLD_MINUTES "$STALE_THRESHOLD_MINUTES"

# ---------------------------------------------------------------------------
# Required contexts come from the checked-in ruleset spec, which is the source of
# truth for what is applied to main. Read via the live API instead would need
# `administration: read`, which this workflow deliberately does not have. Parsed
# fail-loud: an empty or unparseable list would make the remediation hint below
# silently vacuous.
# ---------------------------------------------------------------------------
[ -f "$RULESET" ] || fail "missing ${RULESET}; cannot tell which status checks are required on main."
REQUIRED_CONTEXTS="$(jq -r '
  [.rules[]? | select(.type == "required_status_checks")
             | .parameters.required_status_checks[]?.context] | unique | .[]' "$RULESET")" \
  || fail "could not parse required_status_checks out of ${RULESET}."
[ -n "$REQUIRED_CONTEXTS" ] || fail "${RULESET} declares no required_status_checks contexts; refusing to report a check state this script cannot actually derive."

say "Repo:      ${REPO}"
say "Head:      ${HEAD_BRANCH}"
say "Tap ref:   ${TRACK_BRANCH}:${FORMULA_PATH}"
say "Threshold: ${THRESHOLD_MINUTES} minutes waiting PR / ${STALE_THRESHOLD_MINUTES} minutes stale tap"
say "Required:  $(printf '%s' "$REQUIRED_CONTEXTS" | tr '\n' ' ')"

NOW="$(date -u +%s)"

# Parse an ISO-8601 Z timestamp to epoch seconds. `date -d` is GNU (ubuntu runners);
# BSD `date -j` is the fallback for local macOS runs.
to_epoch() {
  date -u -d "$1" +%s 2>/dev/null || date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$1" +%s
}

# ===========================================================================
# ARM A — is a rolling bump PR waiting on a human past the threshold?
#
# Discovery. NO `|| true` anywhere on this path: an API failure must go red, not
# skip to a green "no waiting PRs". A guard that can only ever be green is the exact
# bug this whole cluster is about.
# ===========================================================================
PRS="$(gh pr list --repo "$REPO" --state open --head "$HEAD_BRANCH" \
        --json number,createdAt,isDraft,headRefOid,url --limit 100)" \
  || fail "could not list open PRs for ${REPO} head=${HEAD_BRANCH}. Treating an unreadable API as 'nothing waiting' would make this guard unfalsifiable, so this is a failure."

# gh exits 0 on success, so an empty or non-array body means something changed under
# us rather than "no results" (which is a literal `[]`).
printf '%s' "$PRS" | jq -e 'type == "array"' >/dev/null \
  || fail "unexpected response listing open PRs (not a JSON array): ${PRS}"

COUNT="$(printf '%s' "$PRS" | jq 'length')"
WAITING=""

# Zero open bump PRs is NOT an exit any more, only an empty arm A. Arm B still has to
# run: "nothing is waiting on a click" and "the tap is current" are different claims,
# and IRO-689 is the case where the first is true and the second is false.
[ "$COUNT" -gt 0 ] || say "No open ${HEAD_BRANCH} PR, so nothing is waiting on a click. Checking whether the tap is stale anyway (arm B)."

while IFS=$'\t' read -r num created draft sha url; do
  [ -n "$num" ] || continue
  created_epoch="$(to_epoch "$created")" \
    || fail "could not parse createdAt '${created}' for PR #${num}."
  age=$(( (NOW - created_epoch) / 60 ))

  if [ "$age" -le "$THRESHOLD_MINUTES" ]; then
    say "PR #${num} open ${age}m, under the ${THRESHOLD_MINUTES}m threshold. Not reporting."
    continue
  fi

  # Only reached for a PR that is already over the threshold, so this extra API call
  # is not on the routine path. Fail loud for the same reason as the list above.
  rollup="$(gh pr view "$num" --repo "$REPO" --json statusCheckRollup)" \
    || fail "PR #${num} is over the threshold but its check rollup could not be read; refusing to report an age without a check state."

  # StatusContext entries carry `.context`, CheckRun entries carry `.name` — a
  # rollup mixes both, and selecting on `.name` alone silently drops legacy
  # statuses. Latest run per context wins; anything not SUCCESS (including a
  # context that never reported at all) reads as not-green.
  missing="$(printf '%s' "$rollup" | jq -r --arg want "$REQUIRED_CONTEXTS" '
    ([.statusCheckRollup[]? | {ctx: (.name // .context // ""), state: (.conclusion // .state // "PENDING")}]
       | map(select(.ctx != "")) | INDEX(.ctx)) as $seen
    | ($want | split("\n") | map(select(length > 0)))
    | map(select(($seen[.].state // "MISSING") != "SUCCESS")
          | "\(.)=\($seen[.].state // "MISSING")") | join(", ")')" \
    || fail "could not evaluate required checks for PR #${num}."

  if [ -n "$missing" ]; then
    verdict="required checks NOT green (${missing})"
  elif [ "$draft" = "true" ]; then
    verdict="all required checks green but the PR is a DRAFT, so it cannot merge"
  else
    verdict="all required checks green — only the approving-review click is missing"
  fi

  WAITING="${WAITING}
  #${num} open ${age}m (threshold ${THRESHOLD_MINUTES}m), head ${sha:0:12}: ${verdict}
    ${url}"
done < <(printf '%s' "$PRS" | jq -r '.[] | [.number, .createdAt, (.isDraft|tostring), .headRefOid, .url] | @tsv')

if [ "$COUNT" -gt 0 ] && [ -z "$WAITING" ]; then
  say "${COUNT} open ${HEAD_BRANCH} PR(s), none past the ${THRESHOLD_MINUTES}m threshold."
fi

# ===========================================================================
# ARM B — is the tap serving an older release than the newest one? (IRO-690)
#
# This is the property the README's "can briefly trail the newest release" claim is
# about, and until IRO-690 nothing measured it. It needs no PR to exist, so it holds
# whatever arm A's PR list says, and it adds no credential: both calls are plain reads
# on a public repo, covered by the run's existing read-only GITHUB_TOKEN.
#
# Same no-`|| true` rule as arm A. An unreadable release endpoint or an unreadable
# formula must go red; rendering either as "in sync" is how a guard becomes decoration.
# ===========================================================================
STALE=""

# `releases/latest` deliberately, not `git tag`: it excludes drafts and prereleases and
# resolves to exactly what a user browsing the repo (and `gh release view`, which
# scripts/update-homebrew-formula.sh uses to pick a tag) calls the latest release.
LATEST="$(gh api "repos/${REPO}/releases/latest" \
            --jq '[.tag_name, .published_at, (.draft|tostring), (.prerelease|tostring)] | @tsv')" \
  || fail "could not read repos/${REPO}/releases/latest. Treating an unreadable release endpoint as 'the tap is current' would make arm B unfalsifiable, so this is a failure."

IFS=$'\t' read -r LATEST_TAG LATEST_PUBLISHED LATEST_DRAFT LATEST_PRERELEASE <<<"$LATEST"
[ -n "${LATEST_TAG:-}" ] && [ -n "${LATEST_PUBLISHED:-}" ] \
  || fail "repos/${REPO}/releases/latest returned no tag_name/published_at pair: ${LATEST}"
# The endpoint guarantees both are false. If one is ever true the endpoint's semantics
# changed under us, and arm B would be comparing against something users cannot install.
[ "${LATEST_DRAFT}" = "false" ] && [ "${LATEST_PRERELEASE}" = "false" ] \
  || fail "releases/latest returned ${LATEST_TAG} with draft=${LATEST_DRAFT} prerelease=${LATEST_PRERELEASE}; refusing to compare the tap against a release users cannot install."

LATEST_VERSION="${LATEST_TAG#v}"
case "$LATEST_VERSION" in
  ''|*[!0-9.]*) fail "latest release tag '${LATEST_TAG}' does not look like v<dotted-version>; arm B cannot compare it to a formula version." ;;
esac

# Raw media type, so there is no base64 hop whose failure could be mistaken for an
# empty formula. Pinned to ?ref= so the answer is "what the tap serves", not "what this
# checkout happens to contain" (see TRACK_BRANCH above). IRO-499's fail-open contents-API
# hazard applies here: no `|| true`.
FORMULA_SRC="$(gh api -H "Accept: application/vnd.github.raw" \
                 "repos/${REPO}/contents/${FORMULA_PATH}?ref=${TRACK_BRANCH}")" \
  || fail "could not read ${FORMULA_PATH} at ${TRACK_BRANCH} in ${REPO}. Arm B cannot tell what the tap is serving, so this is a failure rather than a pass."

# First `version` line only, and no `| head -1`: under `set -o pipefail` head can close
# the pipe before sed finishes and turn a successful parse into exit 141. `q` inside the
# address block does the same job without a second process.
FORMULA_VERSION="$(printf '%s\n' "$FORMULA_SRC" \
  | sed -n '/^  version "/{s/^  version "\([^"]*\)".*/\1/p;q;}')"
[ -n "$FORMULA_VERSION" ] || fail "no \`  version \"...\"\` line found in ${FORMULA_PATH} at ${TRACK_BRANCH}. Either the formula's shape changed (update this parser AND scripts/verify-homebrew-formula.sh, which reads it the same way) or the fetch returned something other than the formula."

say "Tap serves:  ${FORMULA_VERSION} (${TRACK_BRANCH}:${FORMULA_PATH})"
say "Newest rel:  ${LATEST_VERSION} (${LATEST_TAG}, published ${LATEST_PUBLISHED})"

if [ "$FORMULA_VERSION" = "$LATEST_VERSION" ]; then
  say "Tap is in sync with the newest release."
else
  published_epoch="$(to_epoch "$LATEST_PUBLISHED")" \
    || fail "could not parse published_at '${LATEST_PUBLISHED}' for ${LATEST_TAG}."
  stale_min=$(( (NOW - published_epoch) / 60 ))
  older="$(printf '%s\n%s\n' "$FORMULA_VERSION" "$LATEST_VERSION" | sort -V | head -n1)"

  if [ "$older" = "$FORMULA_VERSION" ]; then
    # BEHIND — the ordinary staleness case. Clocked from the release's publication,
    # because that is when the user-visible trail starts: from that moment
    # `brew install` hands out the older binary.
    if [ "$stale_min" -le "$STALE_THRESHOLD_MINUTES" ]; then
      say "Tap trails by ${LATEST_VERSION} but ${LATEST_TAG} has only been published ${stale_min}m, under the ${STALE_THRESHOLD_MINUTES}m threshold. Inside the advertised brief-trail window."
    else
      STALE="the tap has been serving ${FORMULA_VERSION} for ${stale_min}m while ${LATEST_TAG} is the newest release (threshold ${STALE_THRESHOLD_MINUTES}m)"
    fi
  else
    # AHEAD — no grace window. The formula is pinned to a version that releases/latest
    # does not name, so its download URLs point at a release that is not the latest:
    # either it was deleted/unpublished, or it was demoted to a prerelease. Both mean
    # `brew install` is already resolving against something users are not meant to get,
    # and neither heals with time, so waiting out a threshold only delays the alarm.
    STALE="the tap is pinned to ${FORMULA_VERSION}, which is NEWER than the newest release ${LATEST_TAG}. A formula ahead of releases/latest means its download URLs point at a release that was deleted, unpublished, or demoted to a prerelease"
  fi
fi

# ===========================================================================
# Verdict. Both arms are always evaluated and both are always reported, so a run that
# trips one does not hide the other.
# ===========================================================================
if [ -z "$WAITING" ] && [ -z "$STALE" ]; then
  say "Both arms clear: nothing waiting past ${THRESHOLD_MINUTES}m, and the tap serves the newest release. Tap freshness is within the advertised window."
  exit 0
fi

if [ -n "$STALE" ]; then
  # The remediation branches on whether a bump PR exists, and only the applicable branch
  # is printed. Emitting both and letting the reader pick would hand someone "merge the
  # bump PR" as a next action when there is no bump PR to merge.
  if [ "$COUNT" -gt 0 ]; then
    remedy="  A bump PR IS open, so the bump exists and has not landed. Merge it — see the
  waiting-PR error below if it is listed there, and merge it anyway if it is not: being
  under the ${THRESHOLD_MINUTES}m PR threshold does not help once the tap is already past its own."
  else
    remedy="  ZERO open ${HEAD_BRANCH} PRs: nothing is even trying to fix this, which is the IRO-689
  shape. GitHub auto-closes a PR whose head becomes == its base and never reopens it, so
  the rolling bump PR can vanish while the formula stays behind. Look for a
  closed-unmerged one first, then re-cut the bump:
    gh pr list --repo ${REPO} --state closed --head ${HEAD_BRANCH} --limit 5 \\
      --json number,state,mergedAt,url
    scripts/update-homebrew-formula.sh ${LATEST_TAG}   # cosign-verifies SHA256SUMS first
    # then commit Formula/ironclaw.rb onto ${HEAD_BRANCH} and open a PR"
  fi

  cat >&2 <<EOF
::error::The Homebrew tap is STALE: ${STALE}. That breaks the README's "can briefly trail the newest release" claim.

  Tap serves:      ${FORMULA_VERSION}  (${TRACK_BRANCH}:${FORMULA_PATH})
  Newest release:  ${LATEST_VERSION}  (${LATEST_TAG}, published ${LATEST_PUBLISHED})
  Open ${HEAD_BRANCH} PRs: ${COUNT}

${remedy}

  Do NOT hand-edit the formula's version/url/sha256 lines. \`brew-formula-verify\` is a
  required check and re-derives the whole file from the release's cosign-verified
  SHA256SUMS, so a hand-edit fails the gate rather than landing.
EOF
fi

if [ -n "$WAITING" ]; then
  cat >&2 <<EOF
::error::A Homebrew rolling-bump PR has been waiting past the ${THRESHOLD_MINUTES}-minute threshold, so the tap is at risk of trailing the newest release.${WAITING}

  What to do: merge the bump PR. If its required checks are green the only thing
  missing is the approving review — that click is deliberately still human
  (settled on IRO-670). \`brew-formula-verify\` has already re-derived
  Formula/ironclaw.rb from a cosign-verified SHA256SUMS, so there is nothing left
  to eyeball in the diff. Squash-merge it:
    gh api -X PUT repos/${REPO}/pulls/<N>/merge -f merge_method=squash

  If the required checks are NOT green, that is the blocker, not the click; fix or
  re-run them first. Never relax a required check to land a bump.
EOF
fi

printf '%s\n' "  This guard notifies only. It has no write scope and is not a required status check, so it does not block any PR." >&2
exit 1
