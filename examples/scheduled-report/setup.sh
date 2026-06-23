#!/usr/bin/env bash
# Scheduled report agent — see README.md.
# Wires an agent group plus the channel destination it may post its digest into.
# The recurring wake itself is established by the agent via the `schedule_task`
# tool (no external cron); this script sets up the surrounding access + delivery.
set -euo pipefail

# --- edit these for your setup ---------------------------------------------
AGENT="reporter"
POST_CHANNEL="slack"                     # channel type to post the report into
POST_PLATFORM="C0123OPS"                 # the channel/chat id (e.g. a Slack channel)
OWNER="slack:U0OWNER"                    # a human owner who can drive/approve it
# ---------------------------------------------------------------------------

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
: "${IRONCLAW_API_TOKEN:?set IRONCLAW_API_TOKEN to your control-plane API token}"
ic() { ironctl --addr "$ADDR" "$@"; }   # ironctl reads IRONCLAW_API_TOKEN from the env

echo "==> agent group: ${AGENT}"
ic registry agent-group put --id "$AGENT" --name "Scheduled Reporter" --folder "$AGENT"

echo "==> owner who can drive and approve the agent"
ic registry user put --id "$OWNER" --kind person --name "Report Owner"
ic registry member add --user "$OWNER" --agent "$AGENT"

echo "==> destination: allow the agent to post its report into ${POST_CHANNEL}:${POST_PLATFORM}"
ic registry destination add --agent "$AGENT" --channel "$POST_CHANNEL" --platform "$POST_PLATFORM"

echo
echo "Done. Next, ask the agent to schedule its recurring wake (through the gateway), e.g.:"
echo "  \"Every weekday at 09:00, summarize yesterday's deploys and post it to the ops channel.\""
echo "It will call schedule_task(recurrence: daily). Inspect what was created:"
echo "  ironctl --addr ${ADDR} registry destination check --agent ${AGENT} --channel ${POST_CHANNEL} --platform ${POST_PLATFORM}"
