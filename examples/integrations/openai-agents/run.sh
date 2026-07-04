#!/usr/bin/env bash
# openai-agents — an OpenAI Agents SDK agent whose code-execution tool runs inside a
# real, sealed IronClaw sandbox. One command, zero credentials by default.
#
#   examples/integrations/openai-agents/run.sh            # build + up + run + tear down
#   examples/integrations/openai-agents/run.sh --keep     # leave the demo running
#   examples/integrations/openai-agents/run.sh --attach   # use a running control-plane
#
# Default path needs nothing but Docker + python3 (stdlib): a scripted transcript drives
# the sandbox-backed tool with the offline mock provider. For the real SDK runner:
#   pip install openai-agents && export OPENAI_API_KEY=sk-...
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_PY="$SCRIPT_DIR/agent.py"
# shellcheck source=../_driver.sh
source "$SCRIPT_DIR/../_driver.sh"
