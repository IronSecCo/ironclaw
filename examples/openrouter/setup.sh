#!/usr/bin/env bash
# One key, 100+ models, via OpenRouter — see README.md.
# Creates one agent group pinned to the first-class `openrouter` provider (a single
# key reaches Claude, GPT, Llama, Mistral, Gemini, ...), then sends it a chat and
# prints the reply.
#
# Credential-free smoke: run `PROVIDER=mock ./setup.sh` to prove the full sealed
# round-trip with NO key at all against the offline `mock` provider, before pointing
# it at a real OpenRouter model.
#
# Prerequisites (see README.md):
#   1. an OpenRouter key on the control-plane: export OPENROUTER_API_KEY=sk-or-...
#      (skip for the PROVIDER=mock credential-free smoke)
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="router-helper"
PROVIDER="${PROVIDER:-openrouter}"                     # set PROVIDER=mock for a credential-free smoke
MODEL="${MODEL:-anthropic/claude-3.5-sonnet}"          # any vendor/model at openrouter.ai/models
PROMPT="${PROMPT:-Say hello in one short sentence.}"
# ---------------------------------------------------------------------------

# The mock provider needs no model id; pin nothing so the offline mock-agent answers.
if [ "$PROVIDER" = "mock" ]; then MODEL=""; fi

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
TOKEN="$IRONCLAW_API_TOKEN"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

# No API key is set here or anywhere in this script: the OpenRouter key lives ONLY in
# the control-plane environment (OPENROUTER_API_KEY) and is injected host-side into the
# model-proxy. The sandbox never sees it.
echo "==> agent group pinned to the ${PROVIDER} provider: ${AGENT}"
ic agent create --yes \
  --id "$AGENT" --name "Router Helper (OpenRouter)" \
  --provider "$PROVIDER" ${MODEL:+--model "$MODEL"}

echo "==> sending a chat: \"${PROMPT}\""
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$PROMPT" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the reply (up to 120s)"
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
  echo "FAIL: no reply — for openrouter check that OPENROUTER_API_KEY is set on the" >&2
  echo "      control-plane and the model id is valid; or run PROVIDER=mock ./setup.sh." >&2
  echo "      See README.md." >&2
  exit 1
fi
echo "==> reply from '${PROVIDER}${MODEL:+/$MODEL}':"
echo "    ${reply}"
