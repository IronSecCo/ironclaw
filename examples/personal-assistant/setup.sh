#!/usr/bin/env bash
# Personal assistant on Telegram DMs — see README.md.
# Configures one agent group wired to your Telegram direct messages, then walks
# through the mandatory change-approval flow.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="assistant"
TELEGRAM_USER_ID="123456789"            # your numeric Telegram user id
OWNER="telegram:${TELEGRAM_USER_ID}"
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Personal Assistant" --folder "$AGENT"

echo "==> owner: ${OWNER}"
ic registry user put --id "$OWNER" --kind person --name "You"
ic registry role grant --user "$OWNER" --role owner --agent "$AGENT"

echo "==> Telegram DM messaging group"
MG="$(ic registry messaging-group create --channel telegram --platform "$TELEGRAM_USER_ID" | jq -r .ID)"
echo "    messaging-group id: ${MG}"

echo "==> wiring: reply to every message, known senders only, one session per thread"
ic registry wiring create --mg "$MG" --agent "$AGENT" \
  --engage pattern --pattern '.*' --scope known --session per-thread --priority 10

echo "==> allow the agent to deliver back to your DM"
ic registry destination add --agent "$AGENT" --channel telegram --platform "$TELEGRAM_USER_ID"

echo
echo "==> approval flow: every capability change is HELD for a human decision"
CHG="$(ic change submit --kind persona --group "$AGENT" --by "$OWNER" | jq -r .id)"
echo "    submitted change: ${CHG}"
ic change pending
ic change approve "$CHG" --by "$OWNER"

echo
echo "Done. Inspect it:"
echo "  ironctl --addr ${ADDR} registry session list"
echo "  ironctl --addr ${ADDR} audit --limit 20"
