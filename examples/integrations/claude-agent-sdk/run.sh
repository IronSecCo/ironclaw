#!/usr/bin/env bash
# claude-agent-sdk — a Claude Agent SDK agent whose bash tool is backed by a real,
# sealed IronClaw sandbox session. One command, zero credentials by default.
#
#   examples/integrations/claude-agent-sdk/run.sh            # build + up + run + tear down
#   examples/integrations/claude-agent-sdk/run.sh --keep     # leave the demo running
#   examples/integrations/claude-agent-sdk/run.sh --attach   # use a running control-plane
#
# Default path needs nothing but Docker + python3 (stdlib): a scripted transcript drives
# the sandbox-backed bash tool with the offline mock provider. For the real SDK loop:
#   pip install claude-agent-sdk && export ANTHROPIC_API_KEY=sk-ant-...
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_PY="$SCRIPT_DIR/agent.py"
# shellcheck source=../_driver.sh
source "$SCRIPT_DIR/../_driver.sh"
