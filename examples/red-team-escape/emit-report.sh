#!/usr/bin/env bash
# emit-report.sh — turn a red-team-escape run into a versioned, machine-verifiable
# containment report (JSON + human-readable text).
#
# IronClaw's pitch is "isolation you can prove, not just promise." run.sh proves it
# live; this script freezes that proof into a durable artifact so an adopter can
# confirm, for the EXACT version they run, which isolation invariants held and how
# they were proven — without re-running anything. release.yml signs and attaches the
# output to every GitHub Release (IRO-267).
#
# It is deliberately a pure transform with NO Docker/network dependency: it reads the
# harness's result rows on stdin and a handful of metadata env vars, and writes two
# files. That keeps it unit-testable in isolation (see report_test.sh) and keeps the
# trust-critical JSON shape in one reviewable place.
#
# INPUT (stdin): one result row per line, pipe-delimited:
#     VERDICT|ATTACK|EXPECTED|OBSERVED
#   VERDICT is PASS | FAIL | GAP (the same verdicts run.sh prints). ATTACK is the
#   invariant probed, EXPECTED is the assertion that must hold, OBSERVED is what the
#   probe actually saw.
#
# METADATA (env):
#   REPORT_DIR        (required) directory to write the two report files into
#   COMMIT            source commit SHA the sandbox was built from   (default: unknown)
#   VERSION           release tag this report is bound to            (default: dev)
#   SANDBOX_RUNTIME   OCI runtime the sandbox ran under (runc|runsc) (default: unknown)
#   GVISOR_VERSION    runsc --version, when the runtime is gVisor    (default: empty->null)
#   DOCKER_VERSION    Docker Engine server version tested            (default: unknown)
#   KERNEL            `uname -sr` of the host that ran the harness   (default: unknown)
#   SANDBOX_IMAGE     sandbox image ref/digest that was attacked     (default: unknown)
#   GENERATED_AT      RFC3339 UTC timestamp the proof ran            (default: date -u)
#   REPORT_BASENAME   output basename (default: containment-report)
#
# OUTPUT:
#   $REPORT_DIR/$REPORT_BASENAME.json   machine-verifiable report (schemaVersion 1.0)
#   $REPORT_DIR/$REPORT_BASENAME.txt    the same, human-readable
#
# EXIT: 0 always (this is a renderer, not a judge — run.sh owns the pass/fail verdict
#       and its own exit code). The report faithfully records FAILs when they occur.
set -euo pipefail

command -v jq >/dev/null || { echo "emit-report.sh needs jq" >&2; exit 1; }

REPORT_DIR="${REPORT_DIR:?REPORT_DIR must be set (where to write the report)}"
COMMIT="${COMMIT:-unknown}"
VERSION="${VERSION:-dev}"
SANDBOX_RUNTIME="${SANDBOX_RUNTIME:-unknown}"
GVISOR_VERSION="${GVISOR_VERSION:-}"
DOCKER_VERSION="${DOCKER_VERSION:-unknown}"
KERNEL="${KERNEL:-unknown}"
SANDBOX_IMAGE="${SANDBOX_IMAGE:-unknown}"
REPORT_BASENAME="${REPORT_BASENAME:-containment-report}"
# A timestamp is report metadata (when the proof ran), NOT a build input, so it does
# not affect artifact reproducibility. Allow an override for deterministic tests.
GENERATED_AT="${GENERATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

# runc is the laptop/CI demo fallback; runsc is the sealed production gVisor posture.
case "$SANDBOX_RUNTIME" in
  runsc) POSTURE="prod-gvisor" ;;
  runc)  POSTURE="demo-runc" ;;
  *)     POSTURE="unknown" ;;
esac

mkdir -p "$REPORT_DIR"

# slug ATTACK -> a stable id: lowercase, non-alphanumerics collapse to single dashes.
slug() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
}

# Read rows into a JSON array of invariant objects and tally the summary. A GAP is a
# known, tracked relaxation of the demo (runc) path (see run.sh / README), NOT a core
# breach — so only a FAIL flips `overall` to "breached".
invariants='[]'
total=0 passed=0 failed=0 gaps=0
while IFS='|' read -r verdict attack expected observed; do
  [ -n "${verdict:-}" ] || continue
  total=$((total + 1))
  case "$verdict" in
    PASS) passed=$((passed + 1)) ;;
    FAIL) failed=$((failed + 1)) ;;
    GAP)  gaps=$((gaps + 1)) ;;
  esac
  obj="$(jq -nc \
    --arg id "$(slug "$attack")" \
    --arg verdict "$verdict" \
    --arg attack "$attack" \
    --arg assertion "$expected" \
    --arg observed "$observed" \
    '{id:$id, verdict:$verdict, invariant:$attack, assertion:$assertion, observed:$observed}')"
  invariants="$(jq -c --argjson o "$obj" '. + [$o]' <<<"$invariants")"
done

overall="contained"
[ "$failed" -gt 0 ] && overall="breached"

# gVisor version is null unless we actually ran under runsc and captured it.
gvisor_json='null'
[ -n "$GVISOR_VERSION" ] && gvisor_json="$(jq -nc --arg v "$GVISOR_VERSION" '$v')"

json_path="$REPORT_DIR/$REPORT_BASENAME.json"
jq -n \
  --arg commit "$COMMIT" \
  --arg version "$VERSION" \
  --arg generatedAt "$GENERATED_AT" \
  --arg runtime "$SANDBOX_RUNTIME" \
  --argjson gvisor "$gvisor_json" \
  --arg docker "$DOCKER_VERSION" \
  --arg kernel "$KERNEL" \
  --arg image "$SANDBOX_IMAGE" \
  --arg posture "$POSTURE" \
  --argjson total "$total" \
  --argjson passed "$passed" \
  --argjson failed "$failed" \
  --argjson gaps "$gaps" \
  --arg overall "$overall" \
  --argjson invariants "$invariants" \
  '{
    schemaVersion: "1.0",
    report: "ironclaw-containment",
    generatedBy: "examples/red-team-escape (IRO-257 harness)",
    commit: $commit,
    version: $version,
    generatedAt: $generatedAt,
    runtime: {
      sandboxRuntime: $runtime,
      posture: $posture,
      gVisorVersion: $gvisor,
      dockerVersion: $docker,
      kernel: $kernel,
      sandboxImage: $image
    },
    summary: { total: $total, passed: $passed, failed: $failed, gaps: $gaps, overall: $overall },
    invariants: $invariants
  }' > "$json_path"

# Human-readable rendering of the same facts.
txt_path="$REPORT_DIR/$REPORT_BASENAME.txt"
{
  echo "IronClaw containment report"
  echo "==========================="
  echo "version:       $VERSION"
  echo "commit:        $COMMIT"
  echo "generated:     $GENERATED_AT"
  echo "runtime:       $SANDBOX_RUNTIME (posture: $POSTURE)"
  if [ -n "$GVISOR_VERSION" ]; then
    echo "gVisor:        $GVISOR_VERSION"
  fi
  echo "docker:        $DOCKER_VERSION"
  echo "kernel:        $KERNEL"
  echo "sandbox image: $SANDBOX_IMAGE"
  echo "result:        $overall  ($passed passed, $failed failed, $gaps gap(s) of $total)"
  echo
  printf '  %-6s  %-46s  %s\n' "RESULT" "INVARIANT" "OBSERVED"
  printf '  %-6s  %-46s  %s\n' "------" "----------------------------------------------" "--------"
  # Tab-delimit the fields in jq so multi-word invariant/observed/assertion strings
  # survive the read (space-splitting would mangle them).
  jq -r '.invariants[] | [.verdict, .invariant, .observed, .assertion] | @tsv' "$json_path" \
    | while IFS=$'\t' read -r verdict invariant observed assertion; do
        printf '  %-6s  %-46s  %s\n' "$verdict" "$invariant" "$observed"
        printf '          %-46s  (assertion: %s)\n' "" "$assertion"
      done
  echo
  if [ "$overall" = "breached" ]; then
    echo "VERDICT: a CORE containment invariant did NOT hold for this version. Treat as a"
    echo "         security regression — do not trust this release; yank it."
  else
    echo "VERDICT: every core containment invariant held for this version. Any GAP row is a"
    echo "         known, tracked relaxation of the demo (runc) path, not a core breach —"
    echo "         production gVisor closes it (see examples/red-team-escape/README.md)."
  fi
} > "$txt_path"

echo "wrote $json_path"
echo "wrote $txt_path"
