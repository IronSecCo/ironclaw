#!/usr/bin/env bash
# n8n IronClaw Sandbox node -- one-command containment smoke.
#
# Stands up an IronClaw MCP server (`ironctl mcp serve --http`), then drives the
# node's real execution path (the sandbox_exec MCP client the node calls at
# runtime) through one benign command and a battery of escape attempts, printing
# a PASS/FAIL containment table. Exit status is the verdict, so this doubles as a
# CI smoke: non-zero if any escape is not contained.
#
#   examples/integrations/n8n/run.sh            # build ironctl, serve, demo, tear down
#   examples/integrations/n8n/run.sh --keep     # leave the MCP server running
#
# To reuse a server you already have up, set IRONCLAW_MCP_ADDR (see below).
#
# Requires gVisor (runsc). `ironctl mcp serve` defaults to runsc; the box runs a
# real `sh -c` pipeline, and IronClaw's restrictive seccomp profile omits the
# `fork`/`vfork` syscalls that busybox/dash use to spawn subprocesses. gVisor's
# guest kernel handles those internally; under plain runc they hit the host
# seccomp filter and the shell cannot fork. So this smoke SKIPS (does not fake a
# pass) when runsc is not registered with your engine -- install gVisor from
# https://gvisor.dev/docs/user_guide/install/ for a real run. Set
# IRONCLAW_MCP_RUNTIME=runc to force the (non-functional) fallback anyway.
#
# Env overrides (all optional):
#   IRONCLAW_MCP_PORT     loopback port for the MCP server   (default 9111)
#   IRONCLAW_MCP_ADDR     full base URL of a running server  (reuse it; skip launch)
#   IRONCLAW_MCP_RUNTIME  OCI runtime for the box            (default: runsc if available)
#   IRONCLAW_MCP_IMAGE    sandbox image                      (default alpine:3.20)
#   IRONCTL_BIN           prebuilt ironctl to use instead of building from source
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

PORT="${IRONCLAW_MCP_PORT:-9111}"
ADDR="${IRONCLAW_MCP_ADDR:-http://127.0.0.1:$PORT}"
IMAGE="${IRONCLAW_MCP_IMAGE:-alpine:3.20}"

# A caller-supplied address means "reuse a server I already run"; we launch
# nothing and assume that server is on a runtime that can actually fork.
REUSE=0
[ -n "${IRONCLAW_MCP_ADDR:-}" ] && REUSE=1

KEEP=0
for arg in "$@"; do
  case "$arg" in
    --keep)   KEEP=1 ;;
    -h|--help) sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
  esac
done

command -v node >/dev/null || { echo "this smoke needs Node.js 20+ (global fetch)" >&2; exit 1; }
command -v npm  >/dev/null || { echo "this smoke needs npm" >&2; exit 1; }

SERVER_PID=""
teardown() {
  if [ -n "$SERVER_PID" ]; then
    if [ "$KEEP" = 1 ]; then
      echo; echo "==> leaving the MCP server running (--keep), pid $SERVER_PID at $ADDR"
      echo "    stop it with: kill $SERVER_PID"
      return
    fi
    echo; echo "==> stopping the MCP server (pid $SERVER_PID)"
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}

# --- launch our own server (unless reusing one) --------------------------------
if [ "$REUSE" = 0 ]; then
  command -v docker >/dev/null || { echo "this smoke needs Docker (or Podman) to launch sandbox boxes" >&2; exit 1; }

  # Pick the runtime. gVisor (runsc) is required for a real run; without it we
  # skip rather than report a hollow result (see the header).
  RUNTIME="${IRONCLAW_MCP_RUNTIME:-}"
  if [ -z "$RUNTIME" ]; then
    if docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"runsc"'; then
      RUNTIME=runsc
    else
      cat >&2 <<'SKIP'

==> SKIPPED: gVisor (runsc) is not registered with your container engine.

    `ironctl mcp serve` runs an arbitrary `sh -c` pipeline inside the box, and
    IronClaw's restrictive seccomp profile omits fork/vfork -- busybox/dash
    cannot spawn subprocesses under plain runc, so the benign probe would fail
    for a reason unrelated to containment. gVisor's guest kernel handles this;
    install it (https://gvisor.dev/docs/user_guide/install/) and re-run for a
    real containment table, or set IRONCLAW_MCP_RUNTIME=runc to force it anyway.
SKIP
      exit 0
    fi
  fi

  # Build ironctl from THIS checkout so the smoke exercises the current source (a
  # stale ironctl on PATH may predate `mcp serve`). Set IRONCTL_BIN to reuse a
  # binary already built for the exact same commit.
  IRONCTL="${IRONCTL_BIN:-}"
  if [ -z "$IRONCTL" ]; then
    command -v go >/dev/null || { echo "need Go to build ironctl (or set IRONCTL_BIN)" >&2; exit 1; }
    echo "==> building ironctl from source"
    TMPBIN="$(mktemp -d)"
    ( cd "$REPO_ROOT" && CGO_ENABLED=1 go build -o "$TMPBIN/ironctl" ./cmd/ironctl )
    IRONCTL="$TMPBIN/ironctl"
  fi
  [ "$RUNTIME" = runsc ] || echo "note: forcing runtime='$RUNTIME' (not gVisor); the benign probe may fail on fork." >&2

  trap teardown EXIT
  echo "==> starting ironctl mcp serve at $ADDR (runtime=$RUNTIME image=$IMAGE)"
  LOG="$(mktemp)"
  "$IRONCTL" mcp serve --http "127.0.0.1:$PORT" --runtime "$RUNTIME" --image "$IMAGE" >"$LOG" 2>&1 &
  SERVER_PID=$!
else
  echo "==> reusing the MCP server already at $ADDR"
fi

# --- wait for the server to answer ---------------------------------------------
echo -n "==> waiting for the MCP server to be ready"
ready=0
for _ in $(seq 1 30); do
  if [ -n "$SERVER_PID" ] && ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo; echo "FAIL: ironctl mcp serve exited early. Log:" >&2; cat "${LOG:-/dev/null}" >&2; exit 1
  fi
  if curl -fsS -X POST -H 'Content-Type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"ping"}' "$ADDR" >/dev/null 2>&1; then
    ready=1; break
  fi
  echo -n "."; sleep 1
done
echo
[ "$ready" = 1 ] || { echo "FAIL: MCP server never became ready at $ADDR" >&2; \
  [ -n "${LOG:-}" ] && tail -20 "$LOG" >&2; exit 1; }

# --- install node deps and run the containment demo ----------------------------
if [ ! -d "$SCRIPT_DIR/node_modules" ]; then
  echo "==> installing Node dependencies (first run only)"
  ( cd "$SCRIPT_DIR" && npm install --silent --no-audit --no-fund )
fi

echo "==> running the n8n node containment demo against $ADDR"
( cd "$SCRIPT_DIR" && \
  IRONCLAW_MCP_ADDR="$ADDR" IRONCLAW_MCP_AUTH_TOKEN="${IRONCLAW_MCP_AUTH_TOKEN:-}" \
  ./node_modules/.bin/tsx run.ts )
