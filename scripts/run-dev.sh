#!/usr/bin/env bash
# Run the IronClaw control plane locally in dev mode.
#   Console:  http://127.0.0.1:8830/ui/
#   Stop:     Ctrl-C
#
# --dev: loopback bind, no credentials needed, seeds dev-agent + mock-agent (offline),
# and enables MCP (catalog under the state dir, local servers UNISOLATED for dev).
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

go build -o .bin/ic-cp ./cmd/controlplane

ROOT="${IRONCLAW_DEV_ROOT:-/tmp/icdemo}"
mkdir -p "$ROOT/state" "$ROOT/run"

echo "==> IronClaw console:  http://127.0.0.1:8830/ui/   (Ctrl-C to stop)"
exec .bin/ic-cp --dev \
  --api-addr 127.0.0.1:8830 \
  --state-dir "$ROOT/state" \
  --model-proxy-socket "$ROOT/run/modelproxy.sock"
