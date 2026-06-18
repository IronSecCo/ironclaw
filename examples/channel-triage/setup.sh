#!/usr/bin/env bash
# Channel triage bot on Slack — see README.md.
# A quiet triage agent: engages only on @mention, only for known senders, and
# accumulates the messages it ignores so it has context when called in.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="triage"
SLACK_CHANNEL="C0123ABCD"               # the Slack channel id
ONCALL="slack:U0FRONTDESK"              # a known on-call user id
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Triage Bot" --folder "$AGENT"

echo "==> Slack channel messaging group (strict unknown-sender policy)"
MG="$(ic registry messaging-group create \
  --channel slack --platform "$SLACK_CHANNEL" --group --policy strict | jq -r .ID)"
echo "    messaging-group id: ${MG}"

echo "==> known on-call user as a member of ${AGENT}"
ic registry user put --id "$ONCALL" --kind person --name "On-call"
ic registry member add --user "$ONCALL" --agent "$AGENT"

echo "==> wiring: engage on @mention, known senders only, accumulate context, shared session"
ic registry wiring create --mg "$MG" --agent "$AGENT" \
  --engage mention --scope known --ignored accumulate --session shared --priority 5

echo "==> allow the agent to post back into the channel"
ic registry destination add --agent "$AGENT" --channel slack --platform "$SLACK_CHANNEL"

echo
echo "Done. Inspect it:"
echo "  ironctl --addr ${ADDR} registry messaging-group wirings --id ${MG}"
echo "  ironctl --addr ${ADDR} registry access --user ${ONCALL} --agent ${AGENT}"
