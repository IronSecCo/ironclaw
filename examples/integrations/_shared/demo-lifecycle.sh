#!/usr/bin/env bash
# Shared lifecycle helpers for the framework-integration examples.
#
# Source this from an example run.sh, then call:
#   ensure_demo_up "$@"     # parse flags, build image, compose up, wait /healthz
#   setup_venv "$dir"       # create .venv, pip install -r "$dir/requirements.txt"
#
# Requires the sourcing script to have set REPO_ROOT.

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
AGENT="${IRONCLAW_DEMO_AGENT:-mock-agent}"
HEALTH_TIMEOUT="${IRONCLAW_HEALTH_TIMEOUT:-90}"

export IRONCLAW_ADDR="$ADDR" IRONCLAW_API_TOKEN="$TOKEN" IRONCLAW_DEMO_AGENT="$AGENT"

_KEEP=0
_ATTACH=0

compose() { docker compose -f "$REPO_ROOT/docker-compose.demo.yml" "$@"; }

_teardown() {
  [ "$_KEEP" = 1 ] && { echo "==> leaving the demo running (--keep). Stop it with:"; \
                        echo "    docker compose -f docker-compose.demo.yml down"; return; }
  [ "$_ATTACH" = 1 ] && return
  echo "==> tearing the demo down"
  compose down >/dev/null 2>&1 || true
}

ensure_demo_up() {
  for arg in "$@"; do
    case "$arg" in
      --keep)   _KEEP=1 ;;
      --attach) _ATTACH=1 ;;
      -h|--help) sed -n '2,20p' "${BASH_SOURCE[1]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
      *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
    esac
  done

  command -v docker  >/dev/null || { echo "this demo needs Docker" >&2; exit 1; }
  command -v curl    >/dev/null || { echo "this demo needs curl" >&2; exit 1; }
  command -v python3 >/dev/null || { echo "this demo needs python3" >&2; exit 1; }

  if [ "$_ATTACH" = 0 ]; then
    trap _teardown EXIT
    if [ "${SKIP_BUILD:-0}" != 1 ]; then
      echo "==> building the sandbox image (ironclaw-sandbox:latest) -- first run is ~1-2 min"
      bash "$REPO_ROOT/container/build.sh" >/dev/null
    fi
    echo "==> starting the offline demo control-plane (docker compose -f docker-compose.demo.yml up)"
    compose up --build -d >/dev/null
  fi

  echo -n "==> waiting for the control-plane to be ready (up to ${HEALTH_TIMEOUT}s)"
  local ready=0
  for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
    if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
    echo -n "."; sleep 1
  done
  echo
  [ "$ready" = 1 ] || { echo "FAIL: control-plane never became healthy at $ADDR" >&2; exit 1; }
}

setup_venv() {
  local dir="$1"
  local venv="$dir/.venv"
  if [ ! -d "$venv" ]; then
    echo "==> creating Python venv and installing dependencies (first run only)"
    python3 -m venv "$venv"
    # shellcheck disable=SC1091
    source "$venv/bin/activate"
    pip install --quiet --upgrade pip
    pip install --quiet -r "$dir/requirements.txt"
  else
    # shellcheck disable=SC1091
    source "$venv/bin/activate"
  fi
}
