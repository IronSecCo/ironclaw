#!/usr/bin/env bash
# Webhook responder — see README.md.
# Creates the agent group that answers inbound webhooks, and (optionally) a
# `webhook` destination so replies are POSTed to your URL instead of polled.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="responder"
OWNER="webhook:ops"                      # a known sender allowed to invoke it
REPLY_URL=""                             # optional: POST replies here (leave empty to poll)
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Webhook Responder" --folder "$AGENT"

echo "==> known sender allowed to invoke it"
ic registry user put --id "$OWNER" --kind service --name "Webhook Source"
ic registry member add --user "$OWNER" --agent "$AGENT"

if [ -n "$REPLY_URL" ]; then
  echo "==> push replies to ${REPLY_URL} via a webhook destination"
  ic registry destination add --agent "$AGENT" --channel webhook --platform "$REPLY_URL"
else
  echo "==> no REPLY_URL set — poll replies from GET /v1/ui/chat/${AGENT}/messages"
fi

echo
echo "Done. Send an inbound webhook (see README.md):"
echo "  curl -X POST ${ADDR}/v1/ui/chat/send -H \"Authorization: Bearer \$IRONCLAW_API_TOKEN\" \\"
echo "       -H 'Content-Type: application/json' -d '{\"agentGroupID\":\"${AGENT}\",\"text\":\"...\"}'"
