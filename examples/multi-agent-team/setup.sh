#!/usr/bin/env bash
# Multi-agent team in one Slack channel — see README.md.
# Two agent groups share a channel: a frontline responder (engages on @mention)
# and a scribe (engages on the word "summary"), separated by priority.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
SLACK_CHANNEL="C0TEAMROOM"              # the shared Slack channel id
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

echo "==> agent groups: frontline + scribe"
ic registry agent-group put --id frontline --name "Frontline" --folder frontline
ic registry agent-group put --id scribe    --name "Scribe"    --folder scribe

echo "==> shared Slack channel messaging group"
MG="$(ic registry messaging-group create \
  --channel slack --platform "$SLACK_CHANNEL" --group --policy strict | jq -r .ID)"
echo "    messaging-group id: ${MG}"

echo "==> wirings (priority breaks ties)"
# Frontline takes direct mentions.
ic registry wiring create --mg "$MG" --agent frontline \
  --engage mention --scope all --session shared --priority 10
# Scribe watches for "summary" and recaps.
ic registry wiring create --mg "$MG" --agent scribe \
  --engage pattern --pattern 'summary' --scope all --session shared --priority 1

echo "==> delivery destinations"
ic registry destination add --agent frontline --channel slack --platform "$SLACK_CHANNEL"
ic registry destination add --agent scribe    --channel slack --platform "$SLACK_CHANNEL"

echo
echo "Done. Inspect the wirings on the shared channel:"
echo "  ironctl --addr ${ADDR} registry messaging-group wirings --id ${MG}"
