#!/usr/bin/env bash
# run-agent.sh — the engine behind the `IronClaw` composite GitHub Action.
#
# It is a THIN wrapper over the exact zero-credential demo path that
# examples/hello-ironclaw/run.sh exercises: build the sandbox image, bring up the
# offline demo control-plane (docker-compose.demo.yml), send a prompt to a seeded
# agent group through the REAL secured route (engage -> per-session Docker sandbox
# -> encrypted queue -> delivery), and assert the reply comes back. The offline
# `mock` provider makes no network call, so the whole thing runs on a stock runner
# with nothing but Docker + jq + curl and NO secrets.
#
# On top of that round-trip it freezes two artifacts into the report directory so a
# CI run leaves durable, auditable evidence:
#   - ironclaw-run.json       what was asked, what the agent replied, and the verdict
#   - containment-report.*    (opt-in) the signed-able isolation proof for the exact
#                             build under test, produced by the red-team-escape harness
#
# There is no core control-plane change here: everything is driven through the same
# HTTP routes and scripts a human uses. See docs/integrations/ci.md.
#
# Inputs arrive as environment variables set by action.yml:
#   IC_PROMPT        (required) the prompt to send to the agent
#   IC_PROVIDER      model provider           (default: mock)
#   IC_MODEL         model id                 (default: empty -> provider default)
#   IC_AGENT         agent group id to engage (default: mock-agent)
#   IC_CONFIG        optional path to an agent config file (recorded; mock ignores it)
#   IC_REPORT_DIR    directory to write artifacts into (default: $RUNNER_TEMP or ./)
#   IC_CONTAINMENT   'true' to also emit the containment report (default: false)
#   IC_HEALTH_TIMEOUT / IC_REPLY_TIMEOUT   cold-start budgets in seconds
#   SKIP_BUILD=1     skip the sandbox image build (assume it exists)
#   GITHUB_OUTPUT    set by Actions; step outputs (reply/report-path/...) are appended
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# .github/actions/ironclaw/run-agent.sh -> repo root is three levels up. When the
# action is consumed as `uses: IronSecCo/ironclaw/.github/actions/ironclaw@<ref>`,
# GitHub checks the whole repo out under the action path, so the demo compose file,
# the sandbox build script, and the red-team harness are all present here.
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

PROMPT="${IC_PROMPT:?the action requires a non-empty 'prompt' input}"
PROVIDER="${IC_PROVIDER:-mock}"
MODEL="${IC_MODEL:-}"
AGENT="${IC_AGENT:-mock-agent}"
CONFIG="${IC_CONFIG:-}"
CONTAINMENT="${IC_CONTAINMENT:-false}"
REPORT_DIR="${IC_REPORT_DIR:-${RUNNER_TEMP:-$PWD}/ironclaw-report}"

ADDR="${IRONCLAW_ADDR:-http://127.0.0.1:8787}"
TOKEN="${IRONCLAW_API_TOKEN:-ironclaw-demo}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.demo.yml"
HEALTH_TIMEOUT="${IC_HEALTH_TIMEOUT:-90}"
REPLY_TIMEOUT="${IC_REPLY_TIMEOUT:-180}"

command -v docker >/dev/null || { echo "the IronClaw action needs Docker on the runner" >&2; exit 1; }
command -v jq   >/dev/null   || { echo "the IronClaw action needs jq" >&2; exit 1; }
command -v curl >/dev/null   || { echo "the IronClaw action needs curl" >&2; exit 1; }

mkdir -p "$REPORT_DIR"
REPORT_DIR="$(cd "$REPORT_DIR" && pwd)"   # normalise to an absolute path for outputs

# The demo control-plane seeds ONLY the offline `mock` group. A non-mock provider needs
# real credentials + a compatible agent config the demo does not ship, so fail loud and
# closed rather than silently pretend a credentialled run happened on the mock path.
if [ "$PROVIDER" != "mock" ]; then
  echo "error: this action's built-in, credential-free path only supports provider=mock." >&2
  echo "       provider='$PROVIDER' needs model credentials + a compatible agent config;" >&2
  echo "       supply them to a self-hosted control-plane and point IRONCLAW_ADDR at it." >&2
  exit 2
fi

emit_output() {  # emit_output <name> <value>   (multi-line safe)
  local name="$1" value="$2"
  [ -n "${GITHUB_OUTPUT:-}" ] || return 0
  { printf '%s<<__IC_EOF__\n' "$name"; printf '%s\n' "$value"; printf '__IC_EOF__\n'; } >>"$GITHUB_OUTPUT"
}

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }
teardown() { echo "==> tearing the demo control-plane down"; compose down >/dev/null 2>&1 || true; }
trap teardown EXIT

# --- bring up the offline demo control-plane -------------------------------
if [ "${SKIP_BUILD:-0}" != 1 ]; then
  echo "==> building the sandbox image (ironclaw-sandbox:latest) — first run is ~1-2 min"
  bash "$REPO_ROOT/container/build.sh" >/dev/null
fi
echo "==> starting the offline demo control-plane"
compose up --build -d >/dev/null

echo -n "==> waiting for the control-plane to be ready (up to ${HEALTH_TIMEOUT}s)"
ready=0
for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
  if curl -fsS "$ADDR/healthz" >/dev/null 2>&1; then ready=1; break; fi
  echo -n "."; sleep 1
done
echo
[ "$ready" = 1 ] || { echo "error: control-plane never became healthy at $ADDR within ${HEALTH_TIMEOUT}s" >&2; \
                      compose logs --no-color 2>&1 | tail -40 >&2; exit 1; }

# --- run the agent against the prompt --------------------------------------
echo "==> sending the prompt to agent group '$AGENT'"
curl -fsS -X POST "$ADDR/v1/ui/chat/send" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg a "$AGENT" --arg t "$PROMPT" '{agentGroupID:$a, text:$t}')" >/dev/null

echo -n "==> waiting for the agent's reply, up to ${REPLY_TIMEOUT}s (real sandbox launch + encrypted queue round-trip)"
reply=""
for _ in $(seq 1 "$REPLY_TIMEOUT"); do
  # /messages is drain-on-read: the reply text is `.messages[].content` and is returned
  # exactly once, so it must be read from the right field the first time.
  got="$(curl -fsS "$ADDR/v1/ui/chat/$AGENT/messages" \
          -H "Authorization: Bearer $TOKEN" | jq -r '.messages[]?.content // empty')"
  if [ -n "$got" ]; then reply="$got"; break; fi
  echo -n "."; sleep 1
done
echo

if [ -z "$reply" ]; then
  echo "error: no reply within ${REPLY_TIMEOUT}s — the engage -> sandbox -> reply path is broken" >&2
  compose logs --no-color 2>&1 | tail -80 >&2
  exit 1
fi
echo "    agent replied: $reply"

# --- freeze the run report -------------------------------------------------
COMMIT="${GITHUB_SHA:-$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)}"
# A timestamp is run metadata, not a build input, so it does not affect reproducibility.
GENERATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
RUN_REPORT="$REPORT_DIR/ironclaw-run.json"
jq -n \
  --arg provider "$PROVIDER" --arg model "$MODEL" --arg agent "$AGENT" \
  --arg config "$CONFIG" --arg prompt "$PROMPT" --arg reply "$reply" \
  --arg commit "$COMMIT" --arg at "$GENERATED_AT" \
  '{schemaVersion:"1.0", verdict:"PASS", commit:$commit, generatedAt:$at,
    agent:{group:$agent, provider:$provider,
           model:(if ($model|length)>0 then $model else null end),
           config:(if ($config|length)>0 then $config else null end)},
    run:{prompt:$prompt, reply:$reply}}' >"$RUN_REPORT"
# Fail loud, fail closed: a zero-byte or malformed report must stop the run, not pass
# as a green build with no evidence behind it.
jq -e . "$RUN_REPORT" >/dev/null || { echo "error: run report was not written as valid JSON" >&2; exit 1; }
echo "==> wrote run report: $RUN_REPORT"

emit_output reply "$reply"
emit_output run-report "$RUN_REPORT"

# --- optional: freeze the containment proof for this exact build -----------
if [ "$CONTAINMENT" = "true" ]; then
  teardown; trap - EXIT    # the harness owns its own demo lifecycle
  echo "==> running the red-team-escape harness to emit the containment report"
  IRONCLAW_REPORT_DIR="$REPORT_DIR" \
  IRONCLAW_REPORT_COMMIT="$COMMIT" \
  SKIP_BUILD="${SKIP_BUILD:-0}" \
    bash "$REPO_ROOT/examples/red-team-escape/run.sh"
  emit_output containment-report "$REPORT_DIR/containment-report.json"
fi

emit_output report-path "$REPORT_DIR"
echo
echo "PASS ✅  IronClaw ran the agent against the prompt end-to-end with zero credentials."
