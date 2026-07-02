#!/usr/bin/env bash
# Credential-free end-to-end demo of the slack-triage recipe.
#
# Feeds sample channel messages to the offline `mock-agent` (seeded by
# docker-compose.demo.yml) and prints each reply — no model key, no Slack token.
# Bring the demo up first:
#
#   docker compose -f docker-compose.demo.yml up -d --build
set -euo pipefail

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="mock-agent"

command -v jq >/dev/null || { echo "this demo needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }

# Sample messages a triage bot would label in a real channel.
MESSAGES=(
  "the login page 500s when I submit the form"
  "how do I rotate the API token?"
  "can we add dark mode to the dashboard?"
  "PROD IS DOWN, customers can't checkout"
)

send() {
  curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg a "$AGENT" --arg t "$1" '{agentGroupID:$a, text:$t}')" >/dev/null
}

wait_reply() { # poll until the agent replies, or FAIL (non-zero) after ~30s
  # /messages is drain-on-read and the reply text is `.messages[].content` (NOT
  # `.text`, the /chat/send REQUEST field) — reading the wrong key drains the reply
  # to empty and every later poll sees []. Asserting a NON-EMPTY reply here is the
  # exact guard that catches an IRO-279-class regression, so an empty reply must
  # FAIL the script (set -e propagates the non-zero return), never pass silently.
  for _ in $(seq 1 30); do
    out="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
      -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
    if [ -n "$out" ]; then printf '   -> %s\n' "$out"; return 0; fi
    sleep 1
  done
  echo "FAIL: no reply within 30s — the .content round-trip returned empty." >&2
  echo "      is the demo control-plane up and the sandbox image built?" >&2
  return 1
}

echo "Feeding sample channel messages to the triage agent."
echo "(mock echoes each one to prove delivery; a real model returns a label like 'bug'/'urgent')"
echo
for m in "${MESSAGES[@]}"; do
  echo "==> message: $m"
  send "classify this channel message: $m"
  wait_reply
done
