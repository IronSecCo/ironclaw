#!/usr/bin/env bash
# signed-commit.sh: land a GitHub-SIGNED commit on a rolling bot branch.
#
#   GH_TOKEN=... scripts/signed-commit.sh REPO BRANCH BASE_SHA HEADLINE BODY -- PATH...
#
# Builds one commit whose only parent is BASE_SHA, containing the working-tree
# state of every PATH (a PATH that no longer exists on disk becomes a deletion),
# and points BRANCH at it with a single force ref write. Prints the new SHA.
#
# WHY GRAPHQL. The Git Data API (`POST git/commits`) and the Contents API never
# sign, whoever the token belongs to, so every bot commit on brew/track and
# scores/refresh used to read `verification: unsigned` (see docs/pr-review-process.md).
# `createCommitOnBranch` is the one write path GitHub signs: the commit comes back
# `verified: true`, committer `GitHub`, author = the token's identity (the reviewer
# App, or github-actions[bot] under GITHUB_TOKEN). It cannot set a custom author,
# which is why these commits are no longer attributed to the maintainer.
#
# WHY A STAGING REF. createCommitOnBranch only APPENDS to an existing branch at a
# known head. The rolling branch has to be rebuilt on the LIVE main tip (IRO-482),
# and it must never equal main for an instant, or GitHub auto-closes its open PR
# and never reopens it (IRO-689). So the commit is built on a throwaway
# `signing/...` ref created at BASE_SHA, and BRANCH moves ONCE, straight to it.
# A signature belongs to the commit object, not the ref, so it survives the move.
# The staging ref is deleted on every exit path; ci.yml ignores `signing/**` pushes.
#
# Fail-closed: if GitHub hands back an unsigned commit, or one whose parent is not
# BASE_SHA, BRANCH is left exactly where it was and the script exits 1.
set -euo pipefail

die() { echo "::error title=signed-commit::$*" >&2; exit 1; }

[ "$#" -ge 7 ] && [ "$6" = "--" ] || die "usage: $0 REPO BRANCH BASE_SHA HEADLINE BODY -- PATH..."
repo="$1" branch="$2" base_sha="$3" headline="$4" body="$5"
shift 6

staging="signing/${branch//\//-}-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$$"
work="$(mktemp -d)"
staging_created=0
cleanup() {
  if [ "${staging_created}" = 1 ]; then
    gh api -X DELETE "repos/${repo}/git/refs/heads/${staging}" >/dev/null 2>&1 ||
      echo "::warning title=signed-commit::could not delete staging ref ${staging}; remove it by hand" >&2
  fi
  rm -rf "${work}"
}
trap cleanup EXIT

# One JSON object per path. base64 through `tr` rather than `base64 -w0` so the
# script runs the same on GNU and BSD; --rawfile keeps content byte-exact.
: > "${work}/additions.ndjson"
: > "${work}/deletions.ndjson"
for p in "$@"; do
  if [ -f "${p}" ]; then
    base64 < "${p}" | tr -d '\n' > "${work}/b64"
    jq -nc --arg path "${p}" --rawfile contents "${work}/b64" \
      '{path: $path, contents: $contents}' >> "${work}/additions.ndjson"
  else
    jq -nc --arg path "${p}" '{path: $path}' >> "${work}/deletions.ndjson"
  fi
done

gh api -X POST "repos/${repo}/git/refs" -f "ref=refs/heads/${staging}" -f "sha=${base_sha}" >/dev/null
staging_created=1

jq -n \
  --arg repo "${repo}" --arg branch "${staging}" --arg head "${base_sha}" \
  --arg headline "${headline}" --arg body "${body}" \
  --slurpfile additions "${work}/additions.ndjson" \
  --slurpfile deletions "${work}/deletions.ndjson" \
  '{query: "mutation($i: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $i) { commit { oid } } }",
    variables: {i: {
      branch: {repositoryNameWithOwner: $repo, branchName: $branch},
      expectedHeadOid: $head,
      message: {headline: $headline, body: $body},
      fileChanges: {additions: $additions, deletions: $deletions}}}}' \
  > "${work}/request.json"

gh api graphql --input "${work}/request.json" > "${work}/response.json" ||
  die "createCommitOnBranch failed: $(cat "${work}/response.json")"
new_sha="$(jq -r '.data.createCommitOnBranch.commit.oid // empty' "${work}/response.json")"
[ -n "${new_sha}" ] || die "createCommitOnBranch returned no commit: $(cat "${work}/response.json")"

# Read the commit back instead of trusting the mutation: the point of this script
# is the signature, so prove it before the rolling branch moves.
gh api "repos/${repo}/commits/${new_sha}" \
  --jq '[.commit.verification.verified, .commit.verification.reason, .parents[0].sha, (.parents | length)] | @tsv' \
  > "${work}/check.tsv"
IFS=$'\t' read -r verified reason parent nparents < "${work}/check.tsv"
[ "${verified}" = "true" ] || die "commit ${new_sha} is not signed (verification reason: ${reason}); ${branch} left unchanged"
[ "${nparents}" = "1" ] && [ "${parent}" = "${base_sha}" ] ||
  die "commit ${new_sha} has parent ${parent} (${nparents} parents), expected only ${base_sha}; ${branch} left unchanged"

# THE SINGLE REF WRITE. The singular git/ref endpoint matches exactly; the plural
# one prefix-matches and misreports a stale brew/track-v* as brew/track (IRO-318).
if gh api "repos/${repo}/git/ref/heads/${branch}" >/dev/null 2>&1; then
  gh api -X PATCH "repos/${repo}/git/refs/heads/${branch}" -f "sha=${new_sha}" -F "force=true" >/dev/null
else
  gh api -X POST "repos/${repo}/git/refs" -f "ref=refs/heads/${branch}" -f "sha=${new_sha}" >/dev/null
fi

echo "${new_sha}"
