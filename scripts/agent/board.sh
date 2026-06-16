#!/usr/bin/env bash
# IronClaw live claim board — GitHub is the single source of truth for liveness.
#
# Usage:  scripts/agent/board.sh
#
# Prints the genuinely-claimable issues RIGHT NOW (open, agent:ready, no live
# claim ref). Use this to pick work instead of the registry's `status` field,
# which is advisory and can drift. The registry remains the source for deps,
# owned_paths, locks, and acceptance criteria — not for live availability.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$HERE/lib.sh"
require_tools

# Claim refs that currently exist (someone holds these; the label may lag the
# ref because the ref is the strongly-consistent CAS, the label is bookkeeping).
# git ls-remote reads the refs directly and returns empty cleanly when there are
# none (the REST namespace endpoint 404s on an empty namespace).
HELD="$(git ls-remote origin 'refs/agent-claims/issue-*' 2>/dev/null \
        | sed 's#.*refs/agent-claims/issue-##' | paste -sd, - || true)"

echo "Claimable now (open + agent:ready, no live claim ref):"
echo "  held claim refs: ${HELD:-none}"
echo

gh issue list --state open --label agent:ready --limit 100 \
   --json number,title,labels \
   --jq '.[] | "\(.number)\t\(.title)\t[\(.labels|map(.name)|join(","))]"' \
| while IFS=$'\t' read -r num title labels; do
    case ",$HELD," in
      *",$num,"*) echo "  #$num  [CLAIM REF HELD — skip]  $title" ;;
      *)          echo "  #$num  $title  $labels" ;;
    esac
  done
