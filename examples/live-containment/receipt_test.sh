#!/usr/bin/env bash
# receipt_test.sh — unit test for emit-receipt.sh, the shareable-receipt renderer.
#
# Pure and hermetic: no Docker, no network, no control-plane. It feeds emit-receipt.sh
# fixed metadata and asserts the JSON/text/SVG it produces are well-formed, say what
# they should, and carry nothing unsafe to publish. This is what keeps the share
# artifact from silently drifting (IRO-367).
#
#   examples/live-containment/receipt_test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EMIT="$SCRIPT_DIR/emit-receipt.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fail() { echo "FAIL: $1" >&2; exit 1; }
ok()   { echo "ok: $1"; }

# --- case 1: a fully-contained run (3/3) -------------------------------------------
GENERATED_AT="2026-07-05T00:00:00Z" \
RECEIPT_DIR="$WORK/c1" \
CONTAINED=3 TOTAL=3 \
SESSION_ID="ses_01JZABCDEF" VERSION="v0.1.203" POSTURE="demo-runc" \
INVARIANTS="network exfil, host filesystem read, Docker socket takeover" \
bash "$EMIT" >/dev/null

J="$WORK/c1/containment-receipt.json"
T="$WORK/c1/containment-receipt.txt"
S="$WORK/c1/containment-receipt.svg"
[ -f "$J" ] && [ -f "$T" ] && [ -f "$S" ] || fail "case1: expected all three files"
jq -e . "$J" >/dev/null || fail "case1: JSON invalid"

[ "$(jq -r .schemaVersion "$J")" = "1.0" ]                 || fail "case1: schemaVersion"
[ "$(jq -r .version "$J")" = "v0.1.203" ]                  || fail "case1: version"
[ "$(jq -r .sessionId "$J")" = "ses_01JZABCDEF" ]          || fail "case1: sessionId"
[ "$(jq -r .posture "$J")" = "demo-runc" ]                 || fail "case1: posture"
[ "$(jq -r .generatedAt "$J")" = "2026-07-05T00:00:00Z" ]  || fail "case1: generatedAt override"
[ "$(jq -r .summary.blocked "$J")" = "3" ]                 || fail "case1: blocked"
[ "$(jq -r .summary.total "$J")" = "3" ]                   || fail "case1: total"
[ "$(jq -r '.invariantsBlocked | length' "$J")" = "3" ]    || fail "case1: invariant count"
[ "$(jq -r '.invariantsBlocked[0]' "$J")" = "network exfil" ] || fail "case1: first invariant trimmed"

grep -q "3/3 escape attempts BLOCKED" "$T" || fail "case1: txt headline"
grep -q "could NOT escape: 3/3" "$T"       || fail "case1: txt one-liner"
grep -q "v0.1.203" "$T"                     || fail "case1: txt version"

# SVG is well-formed-ish and self-contained (no external fetch, no script).
# (The xmlns="http://www.w3.org/2000/svg" namespace URI is a required declaration,
# not a fetch, so we probe for real remote-load vectors instead.)
grep -q "<svg" "$S"                        || fail "case1: svg root"
grep -q "3/3 contained" "$S"               || fail "case1: svg count text"
grep -q "IronClaw" "$S"                    || fail "case1: svg label"
grep -qiE "<script|<image|xlink:href|href=|src=|url\(http" "$S" && fail "case1: svg must not fetch/script externally"

# Safe-to-publish: no obvious secret/host leakage in any artifact. We target concrete
# credential/host-path forms; descriptive invariant NAMES like "Docker socket takeover"
# are expected copy and must not trip this.
if grep -riE "authorization:|bearer [a-z0-9]|password[=:]|api[_-]?key|/etc/shadow|/var/run/docker\.sock|/host/" "$J" "$T" "$S"; then
  fail "case1: artifact leaks a secret/host token"
fi
ok "case1 contained 3/3: valid JSON+txt+SVG, deterministic, safe to publish"

# --- case 2: empty invariant list is tolerated and one-liner still forms ------------
GENERATED_AT="2026-07-05T00:00:00Z" \
RECEIPT_DIR="$WORK/c2" \
CONTAINED=1 TOTAL=1 SESSION_ID="ses_x" VERSION="dev" \
bash "$EMIT" >/dev/null
J2="$WORK/c2/containment-receipt.json"
[ "$(jq -r '.invariantsBlocked | length' "$J2")" = "0" ] || fail "case2: empty invariants -> empty array"
grep -q "could NOT escape: 1/1" "$WORK/c2/containment-receipt.txt" || fail "case2: one-liner without detail"
ok "case2 no invariant list: renders cleanly with an empty array"

# --- case 3: same inputs (minus timestamp) render byte-identical SVG/JSON -----------
for d in a b; do
  GENERATED_AT="2026-07-05T00:00:00Z" \
  RECEIPT_DIR="$WORK/c3$d" CONTAINED=3 TOTAL=3 SESSION_ID="ses_fixed" \
  VERSION="v0.1.203" POSTURE="demo-runc" INVARIANTS="a,b,c" \
  bash "$EMIT" >/dev/null
done
diff "$WORK/c3a/containment-receipt.svg"  "$WORK/c3b/containment-receipt.svg"  || fail "case3: SVG not deterministic"
diff "$WORK/c3a/containment-receipt.json" "$WORK/c3b/containment-receipt.json" || fail "case3: JSON not deterministic"
ok "case3 determinism: identical inputs -> byte-identical SVG + JSON"

echo "ALL PASS"
