#!/usr/bin/env bash
# Credential-free end-to-end demo of the scheduled-report recipe.
#
# Drives the offline `mock-agent` (seeded by docker-compose.demo.yml) through the
# chat playground — no model key, no real Slack workspace. Bring the demo up first:
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
  # /messages is drain-on-read and the reply text is `.messages[].content` (NOT
  # `.text`, the /chat/send REQUEST field) — reading the wrong key drains the reply
  # to empty and every later poll sees []. Asserting a NON-EMPTY reply here is the
  # exact guard that catches an IRO-279-class regression, so an empty reply must
  # FAIL the script (set -e propagates the non-zero return), never pass silently.
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

echo "==> 1. Ask the reporter to schedule a recurring daily summary (schedule_task tool)"
send 'tool:schedule_task {"prompt":"Post the daily incident summary to #ops","recurrence":"daily"}'
wait_reply

echo "==> 2. Ask it to summarize today's activity"
echo "       (mock echoes to prove the round-trip; a real model would summarize)"
send 'Summarize for #ops: 3 deploys, 1 rollback, 0 incidents today.'
wait_reply

echo
echo "Done. The schedule_task call above is what a real reporter uses to wake itself"
echo "every day; with a model credential set on the control-plane the summary is real."
