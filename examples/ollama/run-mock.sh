#!/usr/bin/env bash
# Credential-free end-to-end smoke for the Ollama recipe.
#
# The real recipe (setup.sh) points an agent group at your LOCAL Ollama, which CI
# has no way to run. So the smoke instead drives the offline `mock-agent` seeded by
# docker-compose.demo.yml and asserts a non-empty reply — proving the sealed
# send/poll round-trip works with NO model, NO key, NO Ollama. Bring the demo up
# first:
#
#   docker compose -f docker-compose.demo.yml up -d --build
#
# For the real zero-credential local run against Ollama, use setup.sh (see README).
set -euo pipefail

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="mock-agent"
PROMPT="${PROMPT:-Say hello in one short sentence.}"

command -v jq >/dev/null || { echo "this demo needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }

echo "==> sending a chat to the offline mock-agent (stands in for your local Ollama): \"${PROMPT}\""
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$PROMPT" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the reply"
# /messages is drain-on-read and the reply text is `.messages[].content` (NOT
# `.text`, the /chat/send REQUEST field). Asserting a NON-EMPTY reply is the guard
# that catches an IRO-279-class regression, so an empty reply FAILS the script.
for _ in $(seq 1 30); do
  out="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
    -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
  if [ -n "$out" ]; then echo; printf '   -> %s\n' "$out"; exit 0; fi
  echo -n "."; sleep 1
done
echo
echo "FAIL: no reply within 30s — the .content round-trip returned empty." >&2
echo "      is the demo control-plane up and the sandbox image built?" >&2
exit 1
