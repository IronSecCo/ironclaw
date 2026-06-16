#!/usr/bin/env bash
# IronClaw atomic task landing — the single terminal step after a push lands.
#
# Usage:  AGENT_ID=claude-host-4f8a scripts/agent/land.sh <issue-number> <commit-sha>
#
# Eliminates "marked done but never closed": this performs the WHOLE terminal
# transition in one place — landed comment, label swap to agent:done, CLOSE the
# issue, and release the atomic claim ref. Run it immediately after
# scripts/agent/push.sh reports the commit on main and CI is green.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$HERE/lib.sh"
require_tools

: "${AGENT_ID:?AGENT_ID is required (e.g. claude-host-4f8a)}"
ISSUE="${1:?usage: land.sh <issue-number> <commit-sha>}"
COMMIT="${2:?usage: land.sh <issue-number> <commit-sha>}"

# Sanity: the commit must actually be on origin/main before we close anything.
git fetch origin main --prune >/dev/null 2>&1 || true
if ! git merge-base --is-ancestor "$COMMIT" origin/main 2>/dev/null; then
  die "refusing to land #$ISSUE: $COMMIT is not an ancestor of origin/main (did the push land?)"
fi

# 1) Structured landed log.
gh issue comment "$ISSUE" --body "$(cat <<EOF
/agent-landed
agent_id: ${AGENT_ID}
issue: ${ISSUE}
commit: ${COMMIT}
result: landed_on_main
EOF
)" >/dev/null

# 2) Terminal labels: clear all in-flight labels, mark done.
remove_labels "$ISSUE" agent:ready agent:claimed agent:in-progress agent:blocked agent:failed
add_labels    "$ISSUE" agent:done

# 3) CLOSE the issue — the step that was missing.
gh issue close "$ISSUE" --reason completed >/dev/null 2>&1 || gh issue close "$ISSUE" >/dev/null

# 4) Release the atomic claim lock so the ref namespace stays clean.
claim_release "$ISSUE"

echo "LANDED + CLOSED #$ISSUE @ ${COMMIT} (agent ${AGENT_ID})"
