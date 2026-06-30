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
#   IRONCLAW_HEALTH_TIMEOUT  seconds to wait for /healthz   (default 90)
#   IRONCLAW_REPLY_TIMEOUT   seconds to wait for the reply  (default 180)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="${IRONCLAW_DEMO_AGENT:-mock-agent}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.demo.yml"

# Cold-start budgets, in seconds. A warm laptop replies in ~2-3s, but the FIRST
# per-session sandbox launch on a fresh CI runner — container boot off a
# just-built image + the SQLCipher queue handshake + the mock-agent process
# starting to poll its inbound queue — is far slower and is what a fixed 45s
# window was missing. These are deliberately generous and env-overridable; the
# job timeout (20 min in CI) is the real backstop. Note the reply itself rides
# the 2s delivery poll, NOT the 60s sweep, so the budget covers the launch, not
# the delivery cadence.
HEALTH_TIMEOUT="${IRONCLAW_HEALTH_TIMEOUT:-90}"
REPLY_TIMEOUT="${IRONCLAW_REPLY_TIMEOUT:-180}"

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
echo -n "==> waiting for the control-plane to be ready (up to ${HEALTH_TIMEOUT}s)"
ready=0
for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
  if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
  echo -n "."; sleep 1
done
echo
[ "$ready" = 1 ] || { echo "FAIL: control-plane never became healthy at $ADDR within ${HEALTH_TIMEOUT}s" >&2; \
                      [ "$ATTACH" = 0 ] && compose logs --no-color 2>&1 | tail -40 >&2; exit 1; }

# --- send a message and assert the reply -----------------------------------
MARKER="hello from hello-ironclaw $$"   # $$ makes the round-trip unmistakable
echo "==> sending a chat message to '$AGENT': \"$MARKER\""
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$MARKER" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the agent's reply, up to ${REPLY_TIMEOUT}s (real sandbox launch + encrypted queue round-trip)"
WANT="mock-agent received: $MARKER"
reply=""
for _ in $(seq 1 "$REPLY_TIMEOUT"); do
  # /messages is drain-on-read: each reply is returned exactly once, so capture it.
  got="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
          -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.text // empty')"
  if [ -n "$got" ]; then reply="$got"; break; fi
  echo -n "."; sleep 1
done
echo

if [ -z "$reply" ]; then
  echo "FAIL: no reply within ${REPLY_TIMEOUT}s — the engage -> sandbox -> reply path is broken" >&2
  if [ "$ATTACH" = 0 ]; then
    echo "--- control-plane logs (tail) ---------------------------------------" >&2
    compose logs --no-color 2>&1 | tail -80 >&2
    # The reply is produced inside the per-session sandbox sibling (ic-sbx-*).
    # If the control-plane launched it but no reply came back, its logs are where
    # a genuine round-trip break shows up — surface them so CI failures are
    # diagnosable without re-running with --keep.
    sbx="$(docker ps -a --filter 'name=ic-sbx-' --format '{{.Names}}' 2>/dev/null || true)"
    if [ -n "$sbx" ]; then
      echo "--- per-session sandbox containers ----------------------------------" >&2
      docker ps -a --filter 'name=ic-sbx-' 2>&1 >&2 || true
      for c in $sbx; do
        echo "--- docker logs $c (tail) -------------------------------------------" >&2
        docker logs "$c" 2>&1 | tail -60 >&2 || true
      done
    else
      echo "(no ic-sbx-* sandbox container found — the per-session sandbox never launched)" >&2
    fi
  fi
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
