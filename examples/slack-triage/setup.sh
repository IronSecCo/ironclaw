#!/usr/bin/env bash
# Slack triage bot — see README.md.
# Classifies/labels EVERY incoming message in a Slack channel: engages on all
# messages (pattern ".") from any sender, and posts a label back into the channel.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="slack-triage"
SLACK_CHANNEL="C0123ABCD"                # the Slack channel id to triage
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Slack Triage" --folder "$AGENT"

echo "==> Slack channel messaging group (public: triage messages from anyone)"
MG="$(ic registry messaging-group create \
  --channel slack --platform "$SLACK_CHANNEL" --group --policy public | jq -r .ID)"
echo "    messaging-group id: ${MG}"

echo "==> wiring: engage on EVERY message, any sender, one shared context"
ic registry wiring create --mg "$MG" --agent "$AGENT" \
  --engage pattern --pattern '.' --scope all --ignored drop --session shared --priority 5

echo "==> allow the agent to post the label back into the channel"
ic registry destination add --agent "$AGENT" --channel slack --platform "$SLACK_CHANNEL"

echo
echo "Done. Inspect it:"
echo "  ironctl --addr ${ADDR} registry messaging-group wirings --id ${MG}"
echo "Then give it a labeling persona via the gateway approval flow (see README.md)."
