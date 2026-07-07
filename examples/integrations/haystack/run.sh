#!/usr/bin/env bash
# Haystack (deepset) + IronClaw sandbox -- one-command demo.
#
# Brings up the offline demo control-plane (mock provider -- no model key, no
# channel tokens), then runs a Haystack agent whose `sandboxed_shell` tool
# executes inside a real IronClaw per-session sandbox. It runs one benign task
# and a battery of escape attempts and prints a PASS/FAIL containment table.
#
#   examples/integrations/haystack/run.sh           # build + up + demo + tear down
#   examples/integrations/haystack/run.sh --keep     # leave the demo running
#   examples/integrations/haystack/run.sh --attach   # use an already-running demo
#
# Env overrides (all optional):
#   IRONCLAW_ADDR        control-plane base URL  (default http://127.0.0.1:8787)
#   IRONCLAW_API_TOKEN   API bearer token        (default ironclaw-demo)
#   IRONCLAW_DEMO_AGENT  agent group id          (default mock-agent)
#   SKIP_BUILD=1         skip the sandbox image build (assume it exists)
#   OPENAI_API_KEY       if set, run a real LLM Haystack Agent instead of the scripted one
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
source "$SCRIPT_DIR/../_shared/demo-lifecycle.sh"

# Haystack supports Python 3.9-3.13; use the caller's PYTHON or the first python3.
if [ -z "${PYTHON:-}" ]; then
  for cand in python3.12 python3.11 python3.10 python3; do
    if command -v "$cand" >/dev/null 2>&1; then PYTHON="$cand"; break; fi
  done
  export PYTHON
fi

ensure_demo_up "$@"
setup_venv "$SCRIPT_DIR"

echo "==> running the Haystack sandbox demo"
python "$SCRIPT_DIR/run.py"
