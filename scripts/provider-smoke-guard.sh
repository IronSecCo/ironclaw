#!/usr/bin/env bash
#
# Provider regression guard (IRO-292).
#
# Every first-class model provider must have a CREDENTIAL-FREE unit/smoke path so
# config drift (a renamed env var, a broken factory default, a dropped host
# injector) is caught in CI instead of shipping. We ship providers fast (Gemini,
# Vertex, Bedrock, Azure, OpenRouter, ...); this script is the anti-rot gate.
#
# It does two things, both credential-free (no provider secret is ever read):
#
#   1. COVERAGE: parses the authoritative Kind* constants from
#      internal/sandbox/provider/provider.go (the factory's source of truth) and
#      asserts every provider kind is referenced by at least one _test.go in the
#      factory package AND — for kinds whose host-side auth needs a dedicated
#      injector — in the model-proxy package. Add a provider without a
#      credential-free test and this fails, by name, listing the gap.
#
#   2. HERMETIC RUN: runs the factory + host-injector test packages with every
#      known provider credential SCRUBBED from the environment, proving the
#      credential-free path actually passes with no secrets present. A test that
#      silently depends on a real key turns this red here.
#
# Mirrors the additive, visible posture of example-smoke.yml (IRO-284): a
# dedicated, named check rather than coverage folded invisibly into `go test ./...`.
#
# Usage: scripts/provider-smoke-guard.sh
# Exit 0 = every first-class provider has credential-free coverage and it passes.

set -euo pipefail

# Resolve the repo root from this script's location so it runs from anywhere.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PROVIDER_PKG="internal/sandbox/provider"
INJECTOR_PKG="internal/host/modelproxy"
PROVIDER_GO="$PROVIDER_PKG/provider.go"

# Kinds whose host-side authentication is injected by a DEDICATED model-proxy
# injector (as opposed to the generic bearer/API-key path shared by
# anthropic/openai/openrouter/gemini/codex). Each of these must additionally have
# a credential-free injector test. (A plain case, not an associative array, so the
# script runs on macOS's bash 3.2 as well as CI's bash 4+.)
#   vertex  — OAuth2 bearer (gcloud ADC / service account)
#   bedrock — AWS SigV4 signing
#   azure   — api-key header or Microsoft Entra bearer
#   local   — optional-key loopback (WithInsecureUpstreams)
needs_injector_test() {
  case "$1" in
    vertex|bedrock|azure|local) return 0 ;;
    *) return 1 ;;
  esac
}

fail=0
note() { printf '  %s\n' "$*"; }

echo "==> Provider regression guard (IRO-292)"
echo "    factory source of truth: $PROVIDER_GO"
echo

# --- 1. COVERAGE ------------------------------------------------------------
# Extract "KindXxx = \"value\"" pairs. The identifier is what tests reference;
# the value is the wire kind string and the injector key. (while-read, not
# mapfile, so this runs on bash 3.2.)
KIND_LINES="$(grep -oE 'Kind[A-Za-z]+[[:space:]]*=[[:space:]]*"[a-z]+"' "$PROVIDER_GO")"

if [ -z "$KIND_LINES" ]; then
  echo "FAIL: no Kind* constants found in $PROVIDER_GO — did the factory move?" >&2
  exit 1
fi

echo "==> Coverage: every provider kind has a credential-free test"
while IFS= read -r line; do
  [ -n "$line" ] || continue
  ident="$(printf '%s' "$line" | grep -oE '^Kind[A-Za-z]+')"  # KindAzure
  val="$(printf '%s' "$line" | grep -oE '"[a-z]+"' | tr -d '"')"  # azure

  # Factory-side coverage: a factory test must CONSTRUCT this kind through New,
  # i.e. contain `Kind: KindXxx` or `Kind: "value"` (case-insensitive, since the
  # factory lower-cases and tests exercise mixed case on purpose).
  if grep -riqE "Kind:[[:space:]]*(\"?${val}\"?|${ident})" "$PROVIDER_PKG"/*_test.go 2>/dev/null; then
    factory="ok"
  else
    factory="MISSING"
    fail=1
  fi

  # Injector-side coverage (only for kinds with a dedicated injector). Match the
  # kind name case-insensitively in the model-proxy tests (e.g. AzureKeyInjector).
  inj="n/a"
  if needs_injector_test "$val"; then
    if grep -riql -- "$val" "$INJECTOR_PKG"/*_test.go 2>/dev/null; then
      inj="ok"
    else
      inj="MISSING"
      fail=1
    fi
  fi

  printf '    %-14s (%-10s) factory=%-7s injector=%s\n' "$ident" "\"$val\"" "$factory" "$inj"
done <<EOF
$KIND_LINES
EOF

if [ "$fail" -ne 0 ]; then
  echo >&2
  echo "FAIL: a provider kind has no credential-free test (see MISSING above)." >&2
  echo "      Add a factory test in $PROVIDER_PKG and, for a dedicated-injector" >&2
  echo "      provider, an injector test in $INJECTOR_PKG before shipping it." >&2
  exit 1
fi
echo "    all provider kinds covered."
echo

# --- 2. HERMETIC RUN --------------------------------------------------------
# Run the credential-free factory + injector tests with EVERY known provider
# credential unset, so a test that leaks a dependency on a real secret fails here
# rather than passing on a developer box that happens to have keys exported.
echo "==> Hermetic run: factory + injector tests with all provider creds scrubbed"
SCRUB=(
  ANTHROPIC_API_KEY OPENAI_API_KEY OPENROUTER_API_KEY
  GOOGLE_API_KEY GEMINI_API_KEY
  GOOGLE_VERTEX_ACCESS_TOKEN GOOGLE_VERTEX_PROJECT GOOGLE_VERTEX_LOCATION GOOGLE_VERTEX_USE_GCLOUD
  AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_REGION AWS_DEFAULT_REGION
  AZURE_OPENAI_API_KEY AZURE_OPENAI_ACCESS_TOKEN AZURE_OPENAI_ENDPOINT AZURE_OPENAI_API_VERSION
  IRONCLAW_LOCAL_MODEL_KEY IRONCLAW_MODEL_GATEWAY_URL
)
unset_args=()
for v in "${SCRUB[@]}"; do unset_args+=(-u "$v"); done

# -count=1 disables the test cache so the scrubbed environment is actually exercised.
env "${unset_args[@]}" go test -count=1 "./$PROVIDER_PKG/..." "./$INJECTOR_PKG/..."

echo
echo "==> Provider smoke guard passed: all first-class providers have a"
echo "    credential-free path and it is green with no secrets present."
