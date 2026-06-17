#!/usr/bin/env bash
# Keyword watcher on Discord — see README.md.
# A quiet ops agent: engages only when a message matches an alert pattern
# (deploy/incident/outage/rollback), from any sender, one session per thread.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="watcher"
DISCORD_CHANNEL="0123456789012345678"   # the Discord channel (snowflake) id
PATTERN='(?i)\b(deploy|incident|outage|rollback)\b'   # what wakes the agent
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Keyword Watcher" --folder "$AGENT"

echo "==> Discord channel messaging group (public policy — anyone can trip the pattern)"
MG="$(ic registry messaging-group create \
  --channel discord --platform "$DISCORD_CHANNEL" --group --policy public | jq -r .ID)"
echo "    messaging-group id: ${MG}"

echo "==> wiring: engage on pattern, any sender, one session per thread"
ic registry wiring create --mg "$MG" --agent "$AGENT" \
  --engage pattern --pattern "$PATTERN" --scope all --session per-thread --priority 5

echo "==> allow the agent to post back into the channel"
ic registry destination add --agent "$AGENT" --channel discord --platform "$DISCORD_CHANNEL"

echo
echo "Done. Inspect it:"
echo "  ironctl --addr ${ADDR} registry messaging-group wirings --id ${MG}"
echo "  ironctl --addr ${ADDR} registry session list"
echo
echo "Remember: set DISCORD_BOT_TOKEN in the control-plane environment so the"
echo "agent can deliver. See docs/channels.md for adapter credentials."
