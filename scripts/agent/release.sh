#!/usr/bin/env bash
# IronClaw claim release — abandon a claim without landing.
#
# Usage:  AGENT_ID=... scripts/agent/release.sh <issue-number> <ready|blocked|failed> [reason...]
#
#   ready   -> return the task to the pool (re-claimable by anyone)
#   blocked -> park it on a dependency/lock (agent:blocked, not claimable)
#   failed  -> attempt failed (agent:failed, not claimable)
#
# Always deletes the atomic claim ref so the task isn't permanently stuck.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$HERE/lib.sh"
require_tools

: "${AGENT_ID:?AGENT_ID is required}"
ISSUE="${1:?usage: release.sh <issue-number> <ready|blocked|failed> [reason...]}"
OUTCOME="${2:?usage: release.sh <issue-number> <ready|blocked|failed> [reason...]}"
shift 2 || true
REASON="$*"

case "$OUTCOME" in
  ready)   NEW_LABEL="agent:ready" ;;
  blocked) NEW_LABEL="agent:blocked" ;;
  failed)  NEW_LABEL="agent:failed" ;;
  *) die "outcome must be one of: ready | blocked | failed" ;;
esac

remove_labels "$ISSUE" agent:claimed agent:in-progress
add_labels    "$ISSUE" "$NEW_LABEL"
claim_release "$ISSUE"

gh issue comment "$ISSUE" --body "$(cat <<EOF
/agent-$([ "$OUTCOME" = ready ] && echo released || echo "$OUTCOME")
agent_id: ${AGENT_ID}
issue: ${ISSUE}
outcome: ${OUTCOME}
reason: ${REASON:-none}
EOF
)" >/dev/null

echo "RELEASED #$ISSUE -> $NEW_LABEL (claim ref freed)"
