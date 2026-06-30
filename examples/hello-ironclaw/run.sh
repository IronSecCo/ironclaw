#!/usr/bin/env bash
# hello-ironclaw — the canonical zero-credential end-to-end check for IronClaw.
#
# One command brings up the offline demo control-plane, sends a chat message
# through the REAL secured path (engage -> per-session Docker sandbox -> encrypted
# queue -> delivery), and ASSERTS the agent's reply comes back. No model key, no
# channel tokens, no gVisor — and a non-zero exit if the round-trip ever breaks.
#
# It doubles as a hermetic CI smoke test (see .github/workflows/example-smoke.yml):
# the offline `mock` provider makes no network call, so the whole pipeline is
# exercisable on a stock runner with nothing but Docker.
#
#   examples/hello-ironclaw/run.sh             # build + up + check + tear down
#   examples/hello-ironclaw/run.sh --keep      # leave the demo running afterwards
#   examples/hello-ironclaw/run.sh --attach     # use an already-running control-plane
#
# Env overrides (all optional):
#   IRONCLAW_ADDR        control-plane base URL   (default http://127.0.0.1:8787)
#   IRONCLAW_API_TOKEN   API bearer token         (default ironclaw-demo)
#   IRONCLAW_DEMO_AGENT  agent group id           (default mock-agent)
#   SKIP_BUILD=1         skip the sandbox image build (assume it exists)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="${IRONCLAW_DEMO_AGENT:-mock-agent}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.demo.yml"

KEEP=0        # --keep: don't tear the demo down on exit
ATTACH=0      # --attach: talk to an already-running control-plane, manage nothing
for arg in "$@"; do
  case "$arg" in
    --keep)   KEEP=1 ;;
    --attach) ATTACH=1 ;;
    -h|--help) sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
  esac
done

command -v jq >/dev/null   || { echo "this check needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }
command -v curl >/dev/null  || { echo "this check needs curl" >&2; exit 1; }

# --- demo lifecycle --------------------------------------------------------
compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

teardown() {
  [ "$KEEP" = 1 ] && { echo "==> leaving the demo running (--keep). Stop it with:"; \
                       echo "    docker compose -f docker-compose.demo.yml down"; return; }
  [ "$ATTACH" = 1 ] && return
  echo "==> tearing the demo down"
  compose down >/dev/null 2>&1 || true
}

if [ "$ATTACH" = 0 ]; then
  command -v docker >/dev/null || { echo "this check needs Docker (or run with --attach)" >&2; exit 1; }
  trap teardown EXIT

  if [ "${SKIP_BUILD:-0}" != 1 ]; then
    echo "==> building the sandbox image (ironclaw-sandbox:latest) — first run is ~1-2 min"
    bash "$REPO_ROOT/container/build.sh" >/dev/null
  fi

  echo "==> starting the offline demo control-plane (docker compose -f docker-compose.demo.yml up)"
  compose up --build -d >/dev/null
fi

# --- wait for /healthz -----------------------------------------------------
echo -n "==> waiting for the control-plane to be ready"
ready=0
for _ in $(seq 1 60); do
  if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
  echo -n "."; sleep 1
done
echo
[ "$ready" = 1 ] || { echo "FAIL: control-plane never became healthy at $ADDR" >&2; \
                      [ "$ATTACH" = 0 ] && compose logs --no-color 2>&1 | tail -40 >&2; exit 1; }

# --- send a message and assert the reply -----------------------------------
MARKER="hello from hello-ironclaw $$"   # $$ makes the round-trip unmistakable
echo "==> sending a chat message to '$AGENT': \"$MARKER\""
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$MARKER" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the agent's reply (real sandbox launch + encrypted queue round-trip)"
WANT="mock-agent received: $MARKER"
reply=""
for _ in $(seq 1 45); do
  # /messages is drain-on-read: each reply is returned exactly once, so capture it.
  got="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
          -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.text // empty')"
  if [ -n "$got" ]; then reply="$got"; break; fi
  echo -n "."; sleep 1
done
echo

if [ -z "$reply" ]; then
  echo "FAIL: no reply within 45s — the engage -> sandbox -> reply path is broken" >&2
  [ "$ATTACH" = 0 ] && compose logs --no-color 2>&1 | tail -60 >&2
  exit 1
fi

echo "    agent replied: $reply"
if [ "$reply" != "$WANT" ]; then
  echo "FAIL: reply did not match expected echo" >&2
  echo "  want: $WANT" >&2
  echo "  got:  $reply" >&2
  exit 1
fi

echo
echo "PASS ✅  IronClaw is working end-to-end with zero credentials."
echo "        message -> engage -> sandboxed mock-agent -> encrypted queue -> reply."
