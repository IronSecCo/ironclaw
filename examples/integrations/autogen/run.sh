#!/usr/bin/env bash
# AutoGen + IronClaw sandbox -- one-command demo.
#
# Brings up the offline demo control-plane (mock provider -- no model key, no
# channel tokens), then runs an AutoGen agent whose `sandboxed_shell` FunctionTool
# executes inside a real IronClaw per-session sandbox. It runs one benign task
# and a battery of escape attempts and prints a PASS/FAIL containment table.
#
#   examples/integrations/autogen/run.sh           # build + up + demo + tear down
#   examples/integrations/autogen/run.sh --keep     # leave the demo running
#   examples/integrations/autogen/run.sh --attach   # use an already-running demo
#
# Env overrides (all optional):
#   IRONCLAW_ADDR        control-plane base URL  (default http://127.0.0.1:8787)
#   IRONCLAW_API_TOKEN   API bearer token        (default ironclaw-demo)
#   IRONCLAW_DEMO_AGENT  agent group id          (default mock-agent)
#   SKIP_BUILD=1         skip the sandbox image build (assume it exists)
#   OPENAI_API_KEY       if set, drive a real LLM agent instead of the scripted one
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
source "$SCRIPT_DIR/../_shared/demo-lifecycle.sh"

# AutoGen supports Python 3.10-3.12. Pick a compatible interpreter unless the
# caller pinned one via PYTHON.
if [ -z "${PYTHON:-}" ]; then
  for cand in python3.12 python3.11 python3.10 python3; do
    if command -v "$cand" >/dev/null 2>&1; then PYTHON="$cand"; break; fi
  done
  export PYTHON
fi

ensure_demo_up "$@"
setup_venv "$SCRIPT_DIR"

echo "==> running the AutoGen sandbox demo"
python "$SCRIPT_DIR/run.py"
