#!/usr/bin/env bash
# examples/smoke-matrix.sh — Release-readiness smoke matrix (IRO-295).
#
# Runs EVERY examples/*/ recipe end-to-end against ONE offline demo control-plane
# and asserts the expected output is really produced, failing loudly on empty or
# incorrect output. It is the developer-facing, single-command equivalent of the
# CI jobs in .github/workflows/example-smoke.yml, EXTENDED to also exercise the
# config-recipe examples (setup.sh) that the chat-only matrix cannot reach — so
# every published example directory has a green (or explicitly-skipped) row.
#
# Zero credentials: every recipe drives the offline `mock` provider, which makes
# no network call, so this needs nothing but Docker + Go + jq + curl.
#
# Why a fresh build matters: the inbound message-seq allocator is fixed in source
# (IRO-278, single authoritative even-seq INSERT), but a STALE control-plane image
# built before that fix will 500 with "UNIQUE constraint failed: messages_in.seq"
# under the rapid multi-message sends in slack-triage. This runner therefore
# rebuilds the demo image from the current checkout by default; pass SKIP_BUILD=1
# only when you know your local images already match HEAD.
#
# Usage:
#   examples/smoke-matrix.sh                # build images, bring demo up, run all, tear down
#   SKIP_BUILD=1 examples/smoke-matrix.sh   # reuse existing images (faster; may be stale)
#   examples/smoke-matrix.sh --attach       # use an already-running demo control-plane
#   examples/smoke-matrix.sh --keep         # leave the demo running afterwards
#
# Exit status is 0 iff every non-skipped recipe passed.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="$REPO_ROOT/docker-compose.demo.yml"
ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
HEALTH_TIMEOUT="${IRONCLAW_HEALTH_TIMEOUT:-120}"

ATTACH=0
KEEP=0
for arg in "$@"; do
  case "$arg" in
    --attach) ATTACH=1 ;;
    --keep)   KEEP=1 ;;
    -h|--help) sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

command -v jq   >/dev/null || { echo "smoke-matrix needs jq (https://jqlang.github.io/jq/)" >&2; exit 1; }
command -v curl >/dev/null || { echo "smoke-matrix needs curl" >&2; exit 1; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

# --- results table ---------------------------------------------------------
declare -a ROWS
record() { ROWS+=("$1|$2|$3"); }   # dir | PASS/FAIL/SKIP | note

# --- lifecycle -------------------------------------------------------------
teardown() {
  [ "$ATTACH" = 1 ] && return
  if [ "$KEEP" = 1 ]; then
    echo "==> leaving the demo running (--keep). Stop it with:"
    echo "    docker compose -f docker-compose.demo.yml down"
    return
  fi
  echo "==> tearing the demo down"
  compose down >/dev/null 2>&1 || true
}

if [ "$ATTACH" = 0 ]; then
  command -v docker >/dev/null || { echo "smoke-matrix needs Docker (or run with --attach)" >&2; exit 1; }
  trap teardown EXIT
  if [ "${SKIP_BUILD:-0}" != 1 ]; then
    echo "==> building the sandbox image (container/build.sh)"
    bash "$REPO_ROOT/container/build.sh" >/dev/null
    echo "==> building the demo control-plane image from the current checkout"
    compose build controlplane >/dev/null
  fi
  echo "==> starting the offline demo control-plane"
  compose up -d >/dev/null
fi

echo -n "==> waiting for the control-plane to be ready (up to ${HEALTH_TIMEOUT}s)"
ready=0
for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
  if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
  echo -n "."; sleep 1
done
echo
if [ "$ready" != 1 ]; then
  echo "FAIL: control-plane never became healthy at $ADDR" >&2
  [ "$ATTACH" = 0 ] && compose logs --no-color 2>&1 | tail -40 >&2
  exit 1
fi

# ironctl is needed by the setup.sh config recipes. Build it once from source so
# the matrix runs on a clean checkout with no prior install.
IRONCTL_BIN="$(command -v ironctl || true)"
if [ -z "$IRONCTL_BIN" ]; then
  echo "==> building ironctl for the config recipes"
  tmpbin="$(mktemp -d)"
  if go build -o "$tmpbin/ironctl" ./cmd/ironctl 2>/dev/null; then
    IRONCTL_BIN="$tmpbin/ironctl"
    export PATH="$tmpbin:$PATH"
  else
    echo "   (warning: could not build ironctl — setup.sh recipes will be SKIPped)" >&2
  fi
fi

# --- runners ---------------------------------------------------------------

# run.sh recipes (hello-ironclaw, red-team-escape) self-assert and support
# --attach so they reuse the shared control-plane instead of managing their own.
run_realpath_recipe() {
  local dir="$1" name; name="$(basename "$dir")"
  echo "::group::$name (run.sh --attach)"
  if bash "$dir/run.sh" --attach; then record "$name" PASS "run.sh round-trip asserted"
  else record "$name" FAIL "run.sh returned non-zero"; fi
  echo "::endgroup::"
}

# run-mock.sh recipes self-assert a NON-EMPTY .content reply (the IRO-279 guard);
# a bare non-zero exit is a real break (empty/wrong reply or broken round-trip).
run_mock_recipe() {
  local dir="$1" name; name="$(basename "$dir")"
  echo "::group::$name (run-mock.sh)"
  if IRONCLAW_ADDR="$ADDR" IRONCLAW_API_TOKEN="$TOKEN" bash "$dir/run-mock.sh"; then
    record "$name" PASS "non-empty .content reply asserted"
  else
    record "$name" FAIL "empty/failed reply assertion"
  fi
  echo "::endgroup::"
}

# setup.sh recipes configure registry objects against the live control-plane.
# They run under `set -euo pipefail`, so any ironctl error aborts non-zero — the
# downstream wiring/destination calls fail loudly if an earlier create returned
# an empty id. We additionally assert the recipe minted a real messaging-group id
# so a silent null cannot pass.
run_setup_recipe() {
  local dir="$1" name; name="$(basename "$dir")"
  echo "::group::$name (setup.sh)"
  if [ -z "${IRONCTL_BIN:-}" ]; then
    record "$name" SKIP "ironctl unavailable"; echo "::endgroup::"; return
  fi
  local out rc
  out="$(IRONCLAW_ADDR="$ADDR" IRONCLAW_API_TOKEN="$TOKEN" bash "$dir/setup.sh" 2>&1)"; rc=$?
  echo "$out"
  if [ "$rc" -ne 0 ]; then
    record "$name" FAIL "setup.sh exit=$rc"
  elif printf '%s\n' "$out" | grep -qE 'messaging-group id: *(mg_[0-9a-f]+)'; then
    record "$name" PASS "config applied, mg id minted"
  else
    record "$name" FAIL "no messaging-group id in output"
  fi
  echo "::endgroup::"
}

# --- dispatch every example directory --------------------------------------
echo "==> running the example smoke matrix"
for dir in "$REPO_ROOT"/examples/*/; do
  dir="${dir%/}"
  name="$(basename "$dir")"
  if   [ -f "$dir/run.sh" ]      && grep -q -- '--attach' "$dir/run.sh"; then run_realpath_recipe "$dir"
  elif [ -f "$dir/run-mock.sh" ]; then run_mock_recipe "$dir"
  elif [ -f "$dir/setup.sh" ];    then run_setup_recipe "$dir"
  else
    # Doc-only recipe (e.g. ci-action, whose green run is proven by the
    # reusable action in .github/workflows/ironclaw-action-example.yml).
    record "$name" SKIP "doc-only; no runnable script"
  fi
done

# --- report ----------------------------------------------------------------
echo
echo "================ example smoke matrix ================"
fail=0 pass=0 skip=0
for row in "${ROWS[@]}"; do
  IFS='|' read -r n s note <<<"$row"
  printf '  %-6s %-20s %s\n' "$s" "$n" "$note"
  case "$s" in PASS) pass=$((pass+1));; FAIL) fail=$((fail+1));; SKIP) skip=$((skip+1));; esac
done
echo "-----------------------------------------------------"
printf '  %d passed, %d failed, %d skipped\n' "$pass" "$fail" "$skip"
echo "====================================================="

[ "$fail" -eq 0 ] || { echo "SMOKE MATRIX RED: $fail example(s) failed" >&2; exit 1; }
echo "SMOKE MATRIX GREEN"
