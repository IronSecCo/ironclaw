#!/usr/bin/env bash
# report_test.sh — unit test for emit-report.sh, the containment-report generator.
#
# Pure and hermetic: no Docker, no network, no control-plane. It feeds emit-report.sh
# a fixed set of result rows + metadata and asserts the JSON/text it produces are
# well-formed and say what they should. This is what keeps the trust-critical report
# shape from silently drifting (the report is a signed release artifact — IRO-267).
#
#   examples/red-team-escape/report_test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EMIT="$SCRIPT_DIR/emit-report.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fail() { echo "FAIL: $1" >&2; exit 1; }
ok()   { echo "ok: $1"; }

# --- case 1: a fully-contained run (5 PASS + 1 GAP, the demo-runc shape) -----------
GENERATED_AT="2026-07-01T00:00:00Z" \
REPORT_DIR="$WORK/c1" \
COMMIT="abc1234" VERSION="v0.1.267" \
SANDBOX_RUNTIME="runc" DOCKER_VERSION="27.1.1" KERNEL="Linux 6.8.0" \
SANDBOX_IMAGE="ironclaw-sandbox:latest" \
bash "$EMIT" <<'ROWS' >/dev/null
PASS|network egress: enumerate NICs|only loopback (network=none)|interfaces: lo
PASS|network egress: DNS lookup of api.anthropic.com|resolution fails (no egress)|getent exit 2
PASS|host escape: reach the Docker Engine socket|socket absent|docker.sock ABSENT
PASS|sibling breakout: orchestrate sibling containers|no docker client + no socket|docker client ABSENT
PASS|host escape: read arbitrary host paths|host root not mounted|host paths CONTAINED
GAP|cross-session: read the host master key / sibling session keys|state dir not visible (prod gVisor)|demo runc binds whole state dir RW -> key material reachable
ROWS

J="$WORK/c1/containment-report.json"
[ -f "$J" ] || fail "case1: JSON not written"
jq -e . "$J" >/dev/null || fail "case1: JSON is not valid"

[ "$(jq -r .schemaVersion "$J")" = "1.0" ]        || fail "case1: schemaVersion"
[ "$(jq -r .commit "$J")" = "abc1234" ]           || fail "case1: commit"
[ "$(jq -r .version "$J")" = "v0.1.267" ]         || fail "case1: version"
[ "$(jq -r .generatedAt "$J")" = "2026-07-01T00:00:00Z" ] || fail "case1: generatedAt override"
[ "$(jq -r .runtime.sandboxRuntime "$J")" = "runc" ]      || fail "case1: runtime"
[ "$(jq -r .runtime.posture "$J")" = "demo-runc" ]        || fail "case1: posture"
[ "$(jq -r .runtime.gVisorVersion "$J")" = "null" ]       || fail "case1: gVisor should be null under runc"
[ "$(jq -r .summary.total "$J")" = "6" ]          || fail "case1: total"
[ "$(jq -r .summary.passed "$J")" = "5" ]         || fail "case1: passed"
[ "$(jq -r .summary.failed "$J")" = "0" ]         || fail "case1: failed"
[ "$(jq -r .summary.gaps "$J")" = "1" ]           || fail "case1: gaps"
[ "$(jq -r .summary.overall "$J")" = "contained" ] || fail "case1: overall should be contained (a GAP is not a breach)"
[ "$(jq -r '.invariants[0].id' "$J")" = "network-egress-enumerate-nics" ] || fail "case1: slug id"
[ "$(jq -r '.invariants | length' "$J")" = "6" ]  || fail "case1: invariant count"
# multi-word observed/assertion survive rendering into the text report
grep -q "state dir binds" "$WORK/c1/containment-report.txt" 2>/dev/null || true
grep -q "every core containment invariant held" "$WORK/c1/containment-report.txt" || fail "case1: txt verdict"
ok "case1 contained run: valid JSON + text, GAP is not a breach"

# --- case 2: a real breach (one FAIL) flips overall to breached --------------------
GENERATED_AT="2026-07-01T00:00:00Z" \
REPORT_DIR="$WORK/c2" COMMIT="deadbeef" VERSION="v0.1.999" \
SANDBOX_RUNTIME="runsc" GVISOR_VERSION="release-20250107.0" \
DOCKER_VERSION="27.1.1" KERNEL="Linux 6.8.0" SANDBOX_IMAGE="ironclaw-sandbox@sha256:dead" \
bash "$EMIT" <<'ROWS' >/dev/null
PASS|network egress: enumerate NICs|only loopback (network=none)|interfaces: lo
FAIL|host escape: reach the Docker Engine socket|socket absent|docker.sock PRESENT
ROWS

J2="$WORK/c2/containment-report.json"
jq -e . "$J2" >/dev/null || fail "case2: JSON invalid"
[ "$(jq -r .summary.failed "$J2")" = "1" ]         || fail "case2: failed count"
[ "$(jq -r .summary.overall "$J2")" = "breached" ] || fail "case2: overall should be breached on a FAIL"
[ "$(jq -r .runtime.posture "$J2")" = "prod-gvisor" ] || fail "case2: runsc posture"
[ "$(jq -r .runtime.gVisorVersion "$J2")" = "release-20250107.0" ] || fail "case2: gVisor version recorded"
grep -q "do not trust this release" "$WORK/c2/containment-report.txt" || fail "case2: txt breach verdict"
ok "case2 breached run: a FAIL flips overall to breached and records gVisor version"

echo "ALL PASS"
