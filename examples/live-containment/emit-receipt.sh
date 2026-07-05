#!/usr/bin/env bash
# emit-receipt.sh — turn a contained live-containment run into a clean, shareable
# "containment receipt": the credibility of a real IronClaw run, frozen into an
# artifact a user can post to X / LinkedIn / their README.
#
# IronClaw's pitch is "isolation you can prove, not just promise." run.sh proves it
# live; this renders that proof into something a user WANTS to share, so every run
# becomes organic inbound (IRO-367). It is deliberately a pure transform with NO
# Docker/network dependency: it reads a handful of metadata env vars and writes
# three files. That keeps it hermetically unit-testable (see receipt_test.sh).
#
# SAFE TO PUBLISH: the artifact carries only counts, the ephemeral session id, the
# public build version, and the invariants that held. No secrets, no host paths, no
# credentials, no PII. The only per-run value is the throwaway session id (a random
# ULID-style token that names a torn-down container), which identifies THIS run and
# nothing else.
#
# METADATA (env):
#   RECEIPT_DIR    (required) directory to write the receipt files into
#   CONTAINED      (required) number of escape attempts that were BLOCKED
#   TOTAL          (required) total escape attempts made
#   SESSION_ID     sandbox session id for this run           (default: unknown)
#   VERSION        IronClaw build the run exercised           (default: unknown)
#   POSTURE        demo-runc | prod-gvisor | unknown          (default: unknown)
#   INVARIANTS     comma/newline list of blocked invariants   (default: empty)
#   REPO_URL       project URL for the share one-liner        (default: github.com/IronSecCo/ironclaw)
#   GENERATED_AT   RFC3339 UTC timestamp (override for tests)  (default: date -u)
#   RECEIPT_BASENAME  output basename                          (default: containment-receipt)
#
# OUTPUT (written into RECEIPT_DIR):
#   $BASENAME.txt   human-readable, copy-pasteable receipt block
#   $BASENAME.json  machine-readable receipt (schemaVersion 1.0)
#   $BASENAME.svg   self-contained shields-style badge (no external fetch)
# It also prints the shareable block + one-liner to stdout.
#
# EXIT: 0 always (this is a renderer, not a judge — run.sh owns the verdict).
set -euo pipefail

command -v jq >/dev/null || { echo "emit-receipt.sh needs jq" >&2; exit 1; }

RECEIPT_DIR="${RECEIPT_DIR:?RECEIPT_DIR must be set (where to write the receipt)}"
CONTAINED="${CONTAINED:?CONTAINED must be set (attempts blocked)}"
TOTAL="${TOTAL:?TOTAL must be set (total attempts)}"
SESSION_ID="${SESSION_ID:-unknown}"
VERSION="${VERSION:-unknown}"
POSTURE="${POSTURE:-unknown}"
INVARIANTS="${INVARIANTS:-}"
REPO_URL="${REPO_URL:-github.com/IronSecCo/ironclaw}"
RECEIPT_BASENAME="${RECEIPT_BASENAME:-containment-receipt}"
# The timestamp is receipt metadata (when the proof ran), not a build input, so it
# does not affect anything reproducible. Allow an override for deterministic tests.
GENERATED_AT="${GENERATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

mkdir -p "$RECEIPT_DIR"

# Normalise the invariant list (comma or newline separated) into a JSON array and a
# comma-joined human string, dropping blanks and trimming whitespace.
inv_json="$(printf '%s' "$INVARIANTS" | tr ',' '\n' \
  | awk '{ gsub(/^[[:space:]]+|[[:space:]]+$/, ""); if (length) print }' \
  | jq -R . | jq -sc .)"
inv_human="$(jq -r 'join(", ")' <<<"$inv_json")"

# ---- JSON receipt ---------------------------------------------------------------
json_path="$RECEIPT_DIR/$RECEIPT_BASENAME.json"
jq -n \
  --arg version "$VERSION" \
  --arg session "$SESSION_ID" \
  --arg posture "$POSTURE" \
  --arg generatedAt "$GENERATED_AT" \
  --arg repo "$REPO_URL" \
  --argjson contained "$CONTAINED" \
  --argjson total "$TOTAL" \
  --argjson invariants "$inv_json" \
  '{
    schemaVersion: "1.0",
    receipt: "ironclaw-containment-receipt",
    generatedBy: "examples/live-containment (IRO-367)",
    version: $version,
    sessionId: $session,
    posture: $posture,
    generatedAt: $generatedAt,
    project: $repo,
    summary: { blocked: $contained, total: $total },
    invariantsBlocked: $invariants
  }' > "$json_path"

# ---- the shareable one-liner ----------------------------------------------------
# Kept plain-text, link-safe, and free of em/en-dashes (public-copy house style).
detail=""
[ -n "$inv_human" ] && detail=" ($inv_human)"
oneliner="I ran a fully-jailbroken AI agent inside IronClaw and it could NOT escape: ${CONTAINED}/${TOTAL} sandbox breakout attempts blocked${detail}. Isolation you can prove, not just promise. ${REPO_URL}"

# ---- human-readable receipt -----------------------------------------------------
txt_path="$RECEIPT_DIR/$RECEIPT_BASENAME.txt"
{
  echo "==============================================================================="
  echo " IRONCLAW CONTAINMENT RECEIPT"
  echo " ${CONTAINED}/${TOTAL} escape attempts BLOCKED. The box held."
  echo "==============================================================================="
  echo " version:    $VERSION"
  echo " session:    $SESSION_ID"
  echo " posture:    $POSTURE"
  echo " generated:  $GENERATED_AT"
  if [ -n "$inv_human" ]; then
    echo " blocked:    $inv_human"
  fi
  echo
  echo " Share this run:"
  echo "   $oneliner"
  echo
  echo " Badge (drop into a README):"
  echo "   ![IronClaw containment]($RECEIPT_BASENAME.svg)"
  echo "==============================================================================="
} > "$txt_path"

# ---- SVG badge ------------------------------------------------------------------
# A self-contained, shields.io-style flat badge: no external fetch, deterministic
# given the run outcome, safe to commit into a README. Left label "IronClaw",
# right "N/M contained" on green (blue-grey if nothing was contained, which the
# live demo never reaches because run.sh exits non-zero on a breach first).
badge_right="${CONTAINED}/${TOTAL} contained"
# Rough monospace width so the two halves size to their text (6px/char + padding).
lw=$(( ${#badge_right} * 7 + 22 ))
llabel="IronClaw"
llw=$(( ${#llabel} * 7 + 22 ))
total_w=$(( llw + lw ))
right_color="#3fb950"   # green: contained
[ "$CONTAINED" -eq 0 ] && right_color="#8b949e"
llx=$(( llw * 10 / 2 ))
rx=$(( llw * 10 + lw * 10 / 2 ))
svg_path="$RECEIPT_DIR/$RECEIPT_BASENAME.svg"
cat > "$svg_path" <<SVG
<svg xmlns="http://www.w3.org/2000/svg" width="$total_w" height="20" role="img" aria-label="IronClaw: $badge_right">
  <title>IronClaw: $badge_right</title>
  <linearGradient id="s" x2="0" y2="100%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="$total_w" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="$llw" height="20" fill="#24292f"/>
    <rect x="$llw" width="$lw" height="20" fill="$right_color"/>
    <rect width="$total_w" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="110" text-rendering="geometricPrecision">
    <text x="$llx" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3">$llabel</text>
    <text x="$llx" y="140" transform="scale(.1)">$llabel</text>
    <text x="$rx" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3">$badge_right</text>
    <text x="$rx" y="140" transform="scale(.1)">$badge_right</text>
  </g>
</svg>
SVG

# ---- print the shareable block to stdout ----------------------------------------
cat "$txt_path"
echo
echo "wrote $txt_path"
echo "wrote $json_path"
echo "wrote $svg_path"
