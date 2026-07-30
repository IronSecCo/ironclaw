#!/usr/bin/env bash
# Compensating check for the machine-merged Homebrew bump (IRO-670).
#
#   scripts/verify-homebrew-formula.sh <BASE_SHA_OR_REF>
#
# The rolling `brew/track` bump PR merges with NO human in the loop. This script is
# the gate that replaced the human click, so it has to be worth more than the click
# was. A maintainer eyeballing a generated single-file diff cannot actually tell a
# correct sha256 from a plausible one; this script can, because it re-derives the
# whole file from a signed source and compares bytes.
#
# What it asserts, in order, all fail-closed:
#
#   1. If the diff against BASE does not touch Formula/ironclaw.rb, there is nothing
#      to verify — pass. (This check is REQUIRED on every PR to main, so it must
#      report success on ordinary PRs rather than being skipped: a required check
#      that never reports blocks the branch forever.)
#   2. If it DOES touch the formula, the diff must touch Formula/ironclaw.rb and
#      NOTHING else. A formula edit smuggled in alongside a code change fails here.
#   3. The release named by the formula's own `version` line has a SHA256SUMS whose
#      cosign signature verifies against the Release workflow's OIDC identity.
#   4. Re-deriving the formula from that verified digest list reproduces the PR head
#      BYTE FOR BYTE. Any difference at all — a flipped sha256, an edited URL, a
#      hand-tweaked caveat, a stale version — fails.
#
# Steps 3 and 4 both live in scripts/update-homebrew-formula.sh, which is the same
# generator the Release workflow runs. There is deliberately ONE derivation path: a
# separate reimplementation here could agree with a wrong generator and prove nothing.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FORMULA_REL="Formula/ironclaw.rb"
FORMULA="${ROOT}/${FORMULA_REL}"

say()  { printf '==> %s\n' "$*"; }
fail() { printf '\nFAIL: %s\n' "$*" >&2; exit 1; }

BASE="${1:-}"
[ -n "$BASE" ] || fail "usage: $0 <BASE_SHA_OR_REF>"

git -C "$ROOT" rev-parse --verify --quiet "$BASE" >/dev/null \
  || fail "base ref '${BASE}' is not resolvable in this checkout. CI must fetch the
  base commit before calling this script (actions/checkout with fetch-depth: 0)."

# --- 1/2: which paths does this PR touch? ------------------------------------
# Two-dot diff (BASE..HEAD, not BASE...HEAD): we want the paths this head actually
# differs from the base on, which is what the merge would write.
#
# Read into an array with a while-read loop rather than `mapfile`/`readarray`: those
# are bash 4+, and macOS still ships bash 3.2, so a maintainer running this locally
# on a Mac would otherwise hit "mapfile: command not found" and read it as the check
# being broken.
CHANGED=()
while IFS= read -r p; do
  [ -n "$p" ] && CHANGED+=("$p")
done < <(git -C "$ROOT" diff --name-only "${BASE}" HEAD)

say "Paths changed vs ${BASE}: ${#CHANGED[@]}"
touches_formula=0
# Guard the expansions on a non-empty array: under `set -u`, bash 3.2 treats
# "${CHANGED[@]}" on an EMPTY array as an unbound variable and aborts. An empty diff
# is a legitimate state (a no-op re-run), not an error.
if [ "${#CHANGED[@]}" -gt 0 ]; then
  for p in "${CHANGED[@]}"; do
    printf '    %s\n' "$p"
    if [ "$p" = "$FORMULA_REL" ]; then
      touches_formula=1
    fi
  done
fi

if [ "$touches_formula" -eq 0 ]; then
  say "No change to ${FORMULA_REL} — nothing for the formula gate to verify."
  say "PASS (not a formula change)"
  exit 0
fi

# The formula is GENERATED. A PR that regenerates it must contain that and only
# that, so the merge a machine performs unattended has exactly one reviewable fact
# in it. Bundling any other path turns an auto-merged PR into an arbitrary-code
# merge with no human and no approval.
if [ "${#CHANGED[@]}" -ne 1 ]; then
  printf '\n' >&2
  for p in "${CHANGED[@]}"; do
    [ "$p" = "$FORMULA_REL" ] || printf '  unexpected path: %s\n' "$p" >&2
  done
  fail "this PR changes ${FORMULA_REL} AND ${#CHANGED[@]} paths in total. A formula
  bump must be formula-only — it is auto-merged without human review, so nothing
  else may ride along. Split the other changes into their own PR."
fi
say "Diff is formula-only. Good."

# --- 3/4: re-derive from a cosign-verified SHA256SUMS and byte-compare -------
# The version the PR head CLAIMS. Everything downstream is derived from the signed
# release for this tag, so a PR that lies about its version simply fails to match.
VERSION="$(sed -n 's/^  version "\([^"]*\)".*/\1/p' "$FORMULA" | head -n1)"
[ -n "$VERSION" ] || fail "could not parse a version line out of ${FORMULA_REL}."
TAG="v${VERSION}"
say "PR head formula claims ${TAG}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

# Re-derivation. update-homebrew-formula.sh cosign-verifies SHA256SUMS (fail-closed,
# pinned to the Release workflow identity) before it reads a single digest, so a
# clean exit here already carries the signature assertion.
say "Re-deriving the expected formula for ${TAG} from its signed SHA256SUMS"
if ! "${ROOT}/scripts/update-homebrew-formula.sh" --out "$tmp/expected.rb" "$TAG" 2>&1 | tee "$tmp/gen.log"; then
  fail "could not re-derive the formula for ${TAG} from a cosign-verified SHA256SUMS.
  See the generator output above — a signature failure there means the release assets
  are untrusted, not that this script is broken."
fi

# Non-vacuity guard. If the generator ever stops verifying the signature — silently
# skipped step, refactor, cosign missing and swallowed somewhere upstream — this
# check must go RED rather than keep passing a byte-comparison against an unverified
# digest list. The whole compensating value of this gate is that assertion.
grep -q '^==> cosign OK — SHA256SUMS' "$tmp/gen.log" \
  || fail "the generator did not report a successful cosign verification of
  SHA256SUMS. Refusing to pass: without that assertion this gate is only comparing
  the formula against an unsigned digest list, which is not a trust anchor."
say "Confirmed: the derivation was anchored to a cosign-verified SHA256SUMS."

# The one comparison in this script. Both the real check below and the self-test
# after it go through THIS function, so the self-test exercises the code path that
# actually gates the merge rather than a lookalike reimplementation of it.
compare() { diff -u "$tmp/expected.rb" "$1"; }

if ! compare "$FORMULA" > "$tmp/formula.diff"; then
  printf '\n--- expected (re-derived from the signed SHA256SUMS of %s)\n' "$TAG" >&2
  printf '+++ this PR head\n' >&2
  sed -n '3,$p' "$tmp/formula.diff" >&2
  fail "the formula in this PR does NOT match what ${TAG}'s signed SHA256SUMS
  produces. Every byte of ${FORMULA_REL} is machine-generated, so any difference is
  either a hand-edit or a corrupted/substituted digest. Do NOT merge. Regenerate with
  'scripts/update-homebrew-formula.sh ${TAG}' and push that."
fi

say "Byte-identical to the re-derived formula."

# --- 5: non-vacuity — prove, on this run, that the gate can still go red ------
# This check is what replaced the human merge click, so "it passed" is only worth
# something if it was capable of failing. A gate that has only ever been observed
# green is indistinguishable from `exit 0` (IRO-670 criterion 5, and the same
# lesson as IRO-656's TYPO negative control).
#
# So don't rely on a one-off scratch-branch experiment that nobody re-runs after
# the next refactor: flip a single hex digit of one sha256 in a COPY of the PR
# head and assert the comparison that just passed now REJECTS it. Every formula
# bump therefore carries its own proof of non-vacuity. If this step ever stops
# failing, compare() has silently stopped comparing and the gate is worthless.
say "Non-vacuity self-test: flipping one sha256 hex digit in a scratch copy"
awk '
  !mutated && match($0, /sha256 "[0-9a-f]+"/) {
    head  = substr($0, 1, RSTART + 7)   # through the opening quote of the digest
    rest  = substr($0, RSTART + 8)      # digest, closing quote, remainder
    first = substr(rest, 1, 1)
    print head (first == "0" ? "1" : "0") substr(rest, 2)
    mutated = 1
    next
  }
  { print }
' "$FORMULA" > "$tmp/corrupt.rb"

# If awk matched nothing the "corrupted" copy is identical to the original and the
# self-test would trivially "detect" a difference that isn't there. Catch that,
# otherwise a future formula template change quietly guts this proof.
if cmp -s "$tmp/corrupt.rb" "$FORMULA"; then
  fail "the self-test could not corrupt a sha256 in ${FORMULA_REL} — no digest
  matched the expected 'sha256 \"<hex>\"' shape. Refusing to pass: this gate can no
  longer demonstrate that it detects a bad digest, so its green is meaningless."
fi

if compare "$tmp/corrupt.rb" >/dev/null 2>&1; then
  fail "SELF-TEST FAILED: the comparison ACCEPTED a formula with a corrupted sha256.
  This gate is vacuous — it would pass a bump carrying an attacker-chosen digest.
  Do not merge anything on the strength of this check until compare() is fixed."
fi
say "Self-test OK — a one-digit corruption is rejected. This gate is not vacuous."

say "PASS: ${FORMULA_REL} at ${TAG} matches its cosign-verified SHA256SUMS exactly."
