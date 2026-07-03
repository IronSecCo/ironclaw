#!/usr/bin/env bash
# Zero-credential local agent, powered by Ollama — see README.md.
# Creates one agent group pinned to the first-class `ollama` provider so it runs
# against a model on your own machine with NO cloud API key anywhere in the stack,
# then sends it a chat and prints the reply.
#
# Prerequisites (see README.md):
#   1. ollama pull llama3.2                 # or set OLLAMA_MODEL
#   2. start the control-plane with --ollama (or IRONCLAW_OLLAMA=1)
#      for a non-default port or a remote Ollama: export OLLAMA_HOST=host:port first
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="local-helper"
MODEL="${OLLAMA_MODEL:-llama3.2}"   # must be pulled first: `ollama pull llama3.2`
PROMPT="${PROMPT:-Say hello in one short sentence.}"
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
TOKEN="$IRONCLAW_API_TOKEN"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

# No API key is set here or anywhere else: the ollama provider needs none. The host
# model-proxy reaches Ollama at OLLAMA_HOST (default localhost:11434) over plain HTTP
# and forwards with no Authorization header. The sandbox never sees a secret because
# there is none.
echo "==> agent group pinned to the ollama provider (zero credential): ${AGENT}"
ic agent create --yes \
  --id "$AGENT" --name "Local Helper (Ollama)" \
  --provider ollama --model "$MODEL"

echo "==> sending a chat: \"${PROMPT}\""
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$PROMPT" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the local model's reply (up to 120s; first token is slow while Ollama loads the model)"
reply=""
for _ in $(seq 1 120); do
  # /messages is drain-on-read: the reply text is `.messages[].content`; read it once.
  got="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
          -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
  if [ -n "$got" ]; then reply="$got"; break; fi
  echo -n "."; sleep 1
done
echo

if [ -z "$reply" ]; then
  echo "FAIL: no reply — check that (1) 'ollama pull ${MODEL}' has been run and" >&2
  echo "      (2) the control-plane was started with --ollama. See README.md." >&2
  exit 1
fi
echo "==> reply from local '${MODEL}' (no credential used anywhere):"
echo "    ${reply}"
