#!/usr/bin/env bash
# _driver.sh — shared lifecycle for the integration examples.
#
# Sourced by openai-agents/run.sh and claude-agent-sdk/run.sh. Brings up the offline
# demo control-plane (zero credentials — the mock provider makes no network call),
# waits for health, runs the caller's Python agent ($AGENT_PY) against a REAL sandbox,
# and tears the demo down. Exits with the agent's exit code, so it doubles as a
# self-checking CI smoke (non-zero if any escape is not contained).
#
# Caller must set AGENT_PY (absolute path to the agent script) before sourcing.
#
# Flags (parsed here): --keep (leave demo up), --attach (use a running control-plane).
# Env overrides: IRONCLAW_ADDR, IRONCLAW_API_TOKEN, IRONCLAW_DEMO_AGENT, SKIP_BUILD=1,
#                IRONCLAW_HEALTH_TIMEOUT (default 90).
set -euo pipefail

: "${AGENT_PY:?_driver.sh requires AGENT_PY to be set}"

REPO_ROOT="$(cd "$(dirname "${AGENT_PY}")/../../.." && pwd)"
ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.demo.yml"
HEALTH_TIMEOUT="${IRONCLAW_HEALTH_TIMEOUT:-90}"

KEEP=0 ATTACH=0
for arg in "$@"; do
  case "$arg" in
    --keep)   KEEP=1 ;;
    --attach) ATTACH=1 ;;
    -h|--help)
      echo "usage: run.sh [--keep] [--attach]"
      echo "  --keep    leave the demo control-plane running afterwards"
      echo "  --attach  talk to an already-running control-plane (manage nothing)"
      exit 0 ;;
    *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
  esac
done

command -v docker >/dev/null || { echo "this example needs Docker" >&2; exit 1; }
command -v python3 >/dev/null || { echo "this example needs python3 (stdlib only)" >&2; exit 1; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

teardown() {
  [ "$KEEP" = 1 ]   && { echo; echo "==> leaving the demo running (--keep). Stop it with:"; \
                         echo "    docker compose -f docker-compose.demo.yml down"; return; }
  [ "$ATTACH" = 1 ] && return
  echo; echo "==> tearing the demo down"
  compose down >/dev/null 2>&1 || true
}

if [ "$ATTACH" = 0 ]; then
  trap teardown EXIT
  if [ "${SKIP_BUILD:-0}" != 1 ]; then
    echo "==> building the sandbox image (ironclaw-sandbox:latest) — first run is ~1-2 min"
    bash "$REPO_ROOT/container/build.sh" >/dev/null
  fi
  echo "==> starting the offline demo control-plane (no API key, no channel tokens)"
  compose up --build -d >/dev/null
fi

echo -n "==> waiting for the control-plane to be ready (up to ${HEALTH_TIMEOUT}s)"
ready=0
for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
  if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
  echo -n "."; sleep 1
done
echo
[ "$ready" = 1 ] || { echo "FAIL: control-plane never became healthy at $ADDR within ${HEALTH_TIMEOUT}s" >&2; \
                      [ "$ATTACH" = 0 ] && compose logs --no-color 2>&1 | tail -40 >&2; exit 1; }

# Hand off to the SDK agent: it engages a real sandbox, runs a benign command through
# the SDK's tool, then proves a jailbroken agent cannot escape. Its exit code is ours.
python3 "$AGENT_PY"
