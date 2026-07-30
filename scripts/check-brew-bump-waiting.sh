#!/usr/bin/env bash
# Freshness guard for the Homebrew tap (IRO-676).
#
#   scripts/check-brew-bump-waiting.sh
#
# Exits non-zero when a rolling `brew/track` bump PR has been sitting open longer
# than THRESHOLD_MINUTES. Run from a schedule (.github/workflows/brew-bump-waiting.yml)
# so a waiting bump ANNOUNCES ITSELF instead of waiting to be found by someone
# sweeping open PRs by hand.
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
# Fire condition: AGE, not check state
# ------------------------------------
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

say()  { printf '==> %s\n' "$*"; }
fail() { printf '::error::%s\n' "$*" >&2; exit 1; }

case "$THRESHOLD_MINUTES" in
  ''|*[!0-9]*) fail "THRESHOLD_MINUTES must be a whole number of minutes, got '${THRESHOLD_MINUTES}'." ;;
esac
[ "$THRESHOLD_MINUTES" -gt 0 ] || fail "THRESHOLD_MINUTES must be > 0; a zero threshold pages on every release the instant the bump PR opens."

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
say "Threshold: ${THRESHOLD_MINUTES} minutes"
say "Required:  $(printf '%s' "$REQUIRED_CONTEXTS" | tr '\n' ' ')"

# ---------------------------------------------------------------------------
# Discovery. NO `|| true` anywhere on this path: an API failure must go red, not
# skip to a green "no waiting PRs". A guard that can only ever be green is the exact
# bug this whole cluster is about.
# ---------------------------------------------------------------------------
PRS="$(gh pr list --repo "$REPO" --state open --head "$HEAD_BRANCH" \
        --json number,createdAt,isDraft,headRefOid,url --limit 100)" \
  || fail "could not list open PRs for ${REPO} head=${HEAD_BRANCH}. Treating an unreadable API as 'nothing waiting' would make this guard unfalsifiable, so this is a failure."

# gh exits 0 on success, so an empty or non-array body means something changed under
# us rather than "no results" (which is a literal `[]`).
printf '%s' "$PRS" | jq -e 'type == "array"' >/dev/null \
  || fail "unexpected response listing open PRs (not a JSON array): ${PRS}"

COUNT="$(printf '%s' "$PRS" | jq 'length')"
if [ "$COUNT" -eq 0 ]; then
  say "No open ${HEAD_BRANCH} PR. The tap is not waiting on a click."
  exit 0
fi

NOW="$(date -u +%s)"
WAITING=""

while IFS=$'\t' read -r num created draft sha url; do
  [ -n "$num" ] || continue
  # `date -d` is GNU (ubuntu runners); fall back to BSD `date -j` for local macOS runs.
  created_epoch="$(date -u -d "$created" +%s 2>/dev/null || date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$created" +%s)" \
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

if [ -z "$WAITING" ]; then
  say "${COUNT} open ${HEAD_BRANCH} PR(s), none past the ${THRESHOLD_MINUTES}m threshold. Tap freshness is within the advertised window."
  exit 0
fi

cat >&2 <<EOF
::error::A Homebrew rolling-bump PR has been waiting past the ${THRESHOLD_MINUTES}-minute threshold. The tap is serving an older release than the newest one, which breaks the README's "can briefly trail the newest release" claim.${WAITING}

  What to do: merge the bump PR. If its required checks are green the only thing
  missing is the approving review — that click is deliberately still human
  (settled on IRO-670). \`brew-formula-verify\` has already re-derived
  Formula/ironclaw.rb from a cosign-verified SHA256SUMS, so there is nothing left
  to eyeball in the diff. Squash-merge it:
    gh api -X PUT repos/${REPO}/pulls/<N>/merge -f merge_method=squash

  If the required checks are NOT green, that is the blocker, not the click; fix or
  re-run them first. Never relax a required check to land a bump.

  This guard notifies only. It has no write scope and is not a required status
  check, so it does not block any PR.
EOF
exit 1
