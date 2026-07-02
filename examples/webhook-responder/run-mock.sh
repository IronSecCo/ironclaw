#!/usr/bin/env bash
# Credential-free end-to-end demo of the webhook-responder recipe.
#
# POSTs an inbound webhook to the offline `mock-agent` (seeded by
# docker-compose.demo.yml) and reads the agent's reply — no model key needed.
# Bring the demo up first:
#
#   docker compose -f docker-compose.demo.yml up -d --build
set -euo pipefail

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="mock-agent"

command -v jq >/dev/null || { echo "this demo needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }

# A sample inbound webhook payload (what an external system would POST).
EVENT='{"source":"ci","event":"deploy_failed","ref":"#1421","env":"prod"}'

echo "==> Inbound webhook -> agent"
echo "    payload: $EVENT"
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "incoming webhook: $EVENT — what should we do?" \
        '{agentGroupID:$a, text:$t}')" >/dev/null

echo "==> Agent reply (polled back; a real model would triage the event):"
for _ in $(seq 1 30); do
  out="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
    -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
  if [ -n "$out" ]; then printf '   %s\n' "$out"; exit 0; fi
  sleep 1
done
echo "   (no reply within 30s — is the demo control-plane up and the sandbox image built?)" >&2
exit 1
