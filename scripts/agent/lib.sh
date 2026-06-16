#!/usr/bin/env bash
# IronClaw agent coordination — shared helpers.
# Sourced by claim.sh / land.sh / release.sh / board.sh.
# Requires: gh (authenticated), git.
set -euo pipefail

# --- repo identity (cached) -------------------------------------------------
_REPO_SLUG=""
repo_slug() {
  if [ -z "$_REPO_SLUG" ]; then
    _REPO_SLUG="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
  fi
  printf '%s' "$_REPO_SLUG"
}

die() { echo "ERROR: $*" >&2; exit 1; }

require_tools() {
  command -v gh  >/dev/null 2>&1 || die "gh CLI not found"
  command -v git >/dev/null 2>&1 || die "git not found"
  gh auth status >/dev/null 2>&1 || die "gh not authenticated (run: gh auth login)"
}

# --- claim lock = a server-side git ref (atomic create / delete) -------------
# The ref namespace 'agent-claims/issue-<n>' is the ONLY thing that makes a
# claim atomic. GitHub's "create a reference" API is a server-side CAS:
# the first creator gets 201, every racing creator gets 422. Labels and
# comments are bookkeeping that only the winner performs.
claim_ref() { printf 'refs/agent-claims/issue-%s' "$1"; }

# claim_acquire <issue> <base_sha>  -> 0 = won, 1 = already held by someone else
claim_acquire() {
  local issue="$1" sha="$2" ref out
  ref="$(claim_ref "$issue")"
  if out="$(gh api -X POST "repos/$(repo_slug)/git/refs" \
            -f ref="$ref" -f sha="$sha" 2>&1)"; then
    return 0
  fi
  if printf '%s' "$out" | grep -qi 'already exists'; then
    return 1
  fi
  die "claim ref create failed for #$issue: $out"
}

# claim_release <issue>  -> deletes the claim ref (idempotent)
claim_release() {
  local issue="$1" ref
  ref="$(claim_ref "$issue")"
  # Delete-ref endpoint is /git/refs/<ref-without-leading-refs/> — the literal
  # 'refs/' segment stays in the URL path, only the prefix token is stripped.
  gh api -X DELETE "repos/$(repo_slug)/git/refs/${ref#refs/}" >/dev/null 2>&1 || true
}

# --- label helpers (tolerant of already-present / already-absent) -----------
add_labels()    { local n="$1"; shift; gh issue edit "$n" $(printf -- '--add-label %q ' "$@") >/dev/null; }
remove_labels() {
  local n="$1"; shift
  local l
  for l in "$@"; do
    gh issue edit "$n" --remove-label "$l" >/dev/null 2>&1 || true
  done
}

issue_state() { gh issue view "$1" --json state -q .state; }
issue_labels() { gh issue view "$1" --json labels -q '[.labels[].name] | join(",")'; }
