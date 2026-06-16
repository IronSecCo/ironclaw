#!/usr/bin/env bash
# IronClaw direct-push CAS landing (AGENTS.md canonical mode).
# Fetch -> rebase origin/main -> preflight -> non-force push -> retry on rejection.
# A rejected push means main moved: rebase, retest, retry. NEVER force-push.
set -euo pipefail

: "${AGENT_ID:?AGENT_ID is required (e.g. claude-host-4f8a)}"

MAX_RETRIES="${MAX_RETRIES:-3}"

for attempt in $(seq 1 "$MAX_RETRIES"); do
  git fetch origin main --prune
  git rebase origin/main
  ./scripts/agent/preflight.sh

  if git push origin HEAD:main; then
    echo "Pushed to main on attempt ${attempt}/${MAX_RETRIES} (agent ${AGENT_ID})."
    exit 0
  fi

  echo "Push rejected — main moved. Rebase + retest + retry ${attempt}/${MAX_RETRIES}."
  sleep $((attempt * 10))
done

echo "Failed to push after ${MAX_RETRIES} retries. Mark the issue agent:blocked and re-plan."
exit 1
