#!/usr/bin/env bash
# IronClaw atomic task claim.
#
# Usage:  AGENT_ID=claude-host-4f8a scripts/agent/claim.sh <issue-number> [scope...]
#
# Eliminates the double-claim race: the claim is won by atomically creating a
# server-side git ref (refs/agent-claims/issue-<n>) via GitHub's create-ref API,
# which is a compare-and-swap. Only the winner touches labels/comments. A losing
# agent exits 1 with ALREADY_CLAIMED and must pick another task.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$HERE/lib.sh"
require_tools

: "${AGENT_ID:?AGENT_ID is required (e.g. claude-host-4f8a)}"
ISSUE="${1:?usage: claim.sh <issue-number> [scope...]}"
shift || true
SCOPE="$*"

# Base must be current origin/main so the work rebases cleanly.
git fetch origin main --prune >/dev/null 2>&1 || true
BASE_SHA="$(git rev-parse origin/main 2>/dev/null || git rev-parse HEAD)"

# 1) Don't even try to claim a closed/ineligible issue.
STATE="$(issue_state "$ISSUE")"
if [ "$STATE" != "OPEN" ]; then
  echo "NOT_CLAIMABLE: #$ISSUE is $STATE"
  exit 1
fi
LABELS="$(issue_labels "$ISSUE")"
case ",$LABELS," in
  *,agent:ready,*) : ;;
  *) echo "NOT_CLAIMABLE: #$ISSUE is not agent:ready (labels: $LABELS)"; exit 1 ;;
esac

# 2) Atomic CAS: create the claim ref. First writer wins.
if ! claim_acquire "$ISSUE" "$BASE_SHA"; then
  echo "ALREADY_CLAIMED: #$ISSUE is held by another agent — pick another task."
  exit 1
fi

# 3) We own it. Re-verify the issue didn't get claimed/closed in the gap, then
#    flip labels and log the structured claim comment. On any failure, release.
trap 'claim_release "$ISSUE"' ERR
LABELS="$(issue_labels "$ISSUE")"
case ",$LABELS," in
  *,agent:claimed,*|*,agent:done,*)
    claim_release "$ISSUE"
    echo "RACE_LOST: #$ISSUE transitioned during claim — releasing."
    exit 1 ;;
esac

remove_labels "$ISSUE" agent:ready
add_labels    "$ISSUE" agent:claimed agent:in-progress

gh issue comment "$ISSUE" --body "$(cat <<EOF
/agent-claim
agent_id: ${AGENT_ID}
base_sha: ${BASE_SHA}
claim_ref: $(claim_ref "$ISSUE")
scope: ${SCOPE:-see task owned_paths}
lease_minutes: 60
EOF
)" >/dev/null
trap - ERR

echo "CLAIMED #$ISSUE by ${AGENT_ID} @ ${BASE_SHA}"
echo "  When done:    AGENT_ID=${AGENT_ID} scripts/agent/land.sh $ISSUE <commit-sha>"
echo "  If abandoned: AGENT_ID=${AGENT_ID} scripts/agent/release.sh $ISSUE blocked|failed"
