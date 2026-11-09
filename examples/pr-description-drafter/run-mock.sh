#!/usr/bin/env bash
# Credential-free end-to-end demo of the pr-description-drafter recipe.
#
# Drives the offline `mock-agent` (seeded by docker-compose.demo.yml) through the
# chat playground — no model key, no real GitHub app. Bring the demo up first:
#
#   docker compose -f docker-compose.demo.yml up -d --build
#
# then run this script. Defaults match the demo (loopback + the fixed demo token).
set -euo pipefail

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="mock-agent"

command -v jq >/dev/null || { echo "this demo needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }

send() { # send <text>
  curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg a "$AGENT" --arg t "$1" '{agentGroupID:$a, text:$t}')" >/dev/null
}

wait_reply() { # poll until the agent replies, or FAIL (non-zero) after ~30s
  for _ in $(seq 1 30); do
    out="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
      -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
    if [ -n "$out" ]; then printf '   %s\n' "$out"; return 0; fi
    sleep 1
  done
  echo "FAIL: no reply within 30s — the .content round-trip returned empty." >&2
  echo "      is the demo control-plane up and the sandbox image built?" >&2
  return 1
}

echo "==> 1. Ask the drafter to summarize a set of commit messages into a PR description"
send 'Please draft a PR description for the following commits: "Fix memory leak in parser", "Add test for parser"'
wait_reply

echo
echo "Done. With a real model credential set on the control-plane, the description is real."
