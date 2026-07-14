#!/usr/bin/env bash
# IronClaw scan action — install ironctl, grade the target, post a sticky PR
# scorecard, and gate on min-score. Read-only and credential-free; the only
# egress is the ironctl release download.
set -euo pipefail

# ---------------------------------------------------------------------------
# Inputs (from action.yml env).
# ---------------------------------------------------------------------------
MODE="${IC_MODE:-container}"
TARGET="${IC_TARGET:-}"
SERVICE="${IC_SERVICE:-}"
MIN_SCORE="${IC_MIN_SCORE:-0}"
COMMENT="${IC_COMMENT:-true}"
BADGE="${IC_BADGE:-false}"
UPLOAD_SARIF="${IC_UPLOAD_SARIF:-false}"
VERSION="${IC_VERSION:-latest}"

REPO="IronSecCo/ironclaw"
WORK="${RUNNER_TEMP:-/tmp}/ironclaw-scan"
mkdir -p "$WORK"

out() { echo "$1=$2" >>"${GITHUB_OUTPUT:-/dev/stdout}"; }
die() { echo "::error::$*" >&2; exit 1; }

[ -n "$TARGET" ] || die "input 'target' is required"
case "$MODE" in
  container|compose|k8s) : ;;
  *) die "input 'mode' must be one of: container | compose | k8s (got '$MODE')" ;;
esac

# ---------------------------------------------------------------------------
# 1. Install ironctl from GitHub Releases (linux/amd64 or arm64).
# ---------------------------------------------------------------------------
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac

# Resolve the tag (vX.Y.Z) and the version (X.Y.Z) used in the archive name.
tag="$VERSION"
if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
  tag="$(gh api "repos/${REPO}/releases/latest" --jq .tag_name 2>/dev/null || true)"
  [ -n "$tag" ] || tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
fi
[ -n "$tag" ] || die "could not resolve the latest ironctl release tag"
ver="${tag#v}"

archive="ironclaw_${ver}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${archive}"
echo "==> installing ironctl ${tag} (${os}/${arch})"
echo "    ${url}"
curl -fsSL "$url" -o "${WORK}/${archive}" || die "download failed: ${url}"
tar -xzf "${WORK}/${archive}" -C "${WORK}" || die "could not extract ${archive}"
IRONCTL="$(find "${WORK}" -maxdepth 2 -type f -name ironctl | head -1)"
[ -n "$IRONCTL" ] || die "archive ${archive} did not contain an 'ironctl' binary"
chmod +x "$IRONCTL"
"$IRONCTL" version || true

# grade_file MODE FILE SERVICE -> echoes the 0-100 score for a compose/k8s file,
# or nothing on any failure. Used to grade the base-branch version of a scanned
# file so the PR comment can show a delta. Fail-open by design.
grade_file() {
  local m="$1" f="$2" svc="$3"
  local a=()
  case "$m" in
    compose) a+=(--compose "$f"); [ -n "$svc" ] && a+=(--service "$svc") ;;
    k8s)     a+=(--k8s "$f") ;;
    *)       return 1 ;;
  esac
  a+=(--md)
  local o
  o="$("$IRONCTL" scan "${a[@]}" 2>/dev/null)" || return 1
  printf '%s' "$o" | sed -nE 's/.*scored \*\*([0-9]+)\/100.*/\1/p' | head -1
}

# ---------------------------------------------------------------------------
# 2. Build the scan arguments for the requested mode.
# ---------------------------------------------------------------------------
scorecard="${WORK}/scorecard.md"
badge_path=""
sarif_path=""
scan_args=()
case "$MODE" in
  container)
    scan_args+=("$TARGET")
    ;;
  compose)
    scan_args+=(--compose "$TARGET")
    [ -n "$SERVICE" ] && scan_args+=(--service "$SERVICE")
    ;;
  k8s)
    scan_args+=(--k8s "$TARGET")
    ;;
esac
scan_args+=(--md)
if [ "$BADGE" = "true" ]; then
  badge_path="${WORK}/scan-badge.svg"
  scan_args+=(--badge "$badge_path")
fi
if [ "$UPLOAD_SARIF" = "true" ]; then
  sarif_path="${WORK}/ironclaw-scan.sarif"
  scan_args+=(--sarif "$sarif_path")
fi

# ---------------------------------------------------------------------------
# 3. Run the scan. Capture the full output; we do NOT pass --min-score so the
#    binary always exits 0 here — the gate runs in bash after we comment, so a
#    failing score still gets a scorecard posted.
# ---------------------------------------------------------------------------
raw="${WORK}/scan.out"
set +e
"$IRONCTL" scan "${scan_args[@]}" >"$raw" 2>&1
rc=$?
set -e
cat "$raw"
if [ "$rc" -ne 0 ]; then
  die "ironctl scan failed (exit $rc). See output above."
fi

# The markdown block starts at the RenderMarkdown header line; slice from there.
sed -n '/^### IronClaw containment scan:/,$p' "$raw" >"$scorecard"
[ -s "$scorecard" ] || die "could not extract the markdown scorecard from scan output"

# Parse the score/grade from the header: "... scored **NN/100 (grade X)**".
score="$(sed -nE 's/.*scored \*\*([0-9]+)\/100.*/\1/p' "$scorecard" | head -1)"
grade="$(sed -nE 's/.*\(grade ([A-F])\).*/\1/p' "$scorecard" | head -1)"
[ -n "$score" ] || die "could not parse the containment score"
echo "==> containment score: ${score}/100 (grade ${grade:-?})"

out score "$score"
out grade "${grade:-?}"
out scorecard "$scorecard"
[ -n "$badge_path" ] && out badge-path "$badge_path"
# Only advertise the SARIF path when the file was actually written, so the
# upload step's guard is precise and never uploads a missing file.
[ -n "$sarif_path" ] && [ -s "$sarif_path" ] && out sarif-path "$sarif_path"

# ---------------------------------------------------------------------------
# 4. Sticky PR comment (create-or-update). A stable marker keyed by mode+target
#    lets one PR carry several scorecards without clobbering each other.
# ---------------------------------------------------------------------------
pr=""
if [ -f "${GITHUB_EVENT_PATH:-/nonexistent}" ]; then
  pr="$(jq -r '.pull_request.number // .issue.number // empty' "$GITHUB_EVENT_PATH" 2>/dev/null || true)"
fi
if [ "$COMMENT" = "true" ] && [ -n "$pr" ] && [ -n "${GH_TOKEN:-}" ]; then
  marker="<!-- ironclaw-scan-scorecard:${MODE}:${TARGET} -->"
  body="${WORK}/comment.md"

  # Base-branch delta (compose/k8s file modes only). Fetch the base version of
  # the scanned file via the contents API — no extra checkout depth needed — and
  # grade it, so the comment shows how this PR moved the posture. Container mode
  # has no git base to compare against, so it is skipped. Entirely fail-open: any
  # hiccup just omits the delta line and never touches the scorecard or gate.
  delta_line=""
  if [ "$MODE" = "compose" ] || [ "$MODE" = "k8s" ]; then
    base_ref="$(jq -r '.pull_request.base.ref // empty' "$GITHUB_EVENT_PATH" 2>/dev/null || true)"
    base_sha="$(jq -r '.pull_request.base.sha // empty' "$GITHUB_EVENT_PATH" 2>/dev/null || true)"
    if [ -n "$base_sha" ]; then
      base_file="${WORK}/base-target"
      if gh api "repos/${GITHUB_REPOSITORY}/contents/${TARGET}?ref=${base_sha}" \
           --jq '.content' 2>/dev/null | base64 -d >"$base_file" 2>/dev/null && [ -s "$base_file" ]; then
        base_score="$(grade_file "$MODE" "$base_file" "$SERVICE" || true)"
        if [ -n "$base_score" ]; then
          d=$(( score - base_score ))
          if [ "$d" -gt 0 ]; then
            delta_line="> **Δ vs base (\`${base_ref:-base}\`): +${d}** — base scored ${base_score}/100. Posture improved. :arrow_up:"
          elif [ "$d" -lt 0 ]; then
            delta_line="> **Δ vs base (\`${base_ref:-base}\`): ${d}** — base scored ${base_score}/100. Posture regressed. :warning:"
          else
            delta_line="> **Δ vs base (\`${base_ref:-base}\`): 0** — unchanged from ${base_score}/100."
          fi
        fi
      else
        delta_line="> _New file on this PR — no base version to compare against._"
      fi
    fi
  fi

  { echo "$marker"; cat "$scorecard"; [ -n "$delta_line" ] && { echo; echo "$delta_line"; }; } >"$body"

  # Find an existing comment carrying our marker and update it; else create one.
  existing="$(gh api "repos/${GITHUB_REPOSITORY}/issues/${pr}/comments" --paginate \
      --jq "map(select(.body | contains(\"${marker}\"))) | .[0].id // empty" 2>/dev/null || true)"
  if [ -n "$existing" ]; then
    gh api -X PATCH "repos/${GITHUB_REPOSITORY}/issues/comments/${existing}" \
      -f body="$(cat "$body")" >/dev/null \
      && echo "==> updated sticky scorecard comment (#${existing})" \
      || echo "::warning::could not update the scorecard comment"
  else
    gh api -X POST "repos/${GITHUB_REPOSITORY}/issues/${pr}/comments" \
      -f body="$(cat "$body")" >/dev/null \
      && echo "==> posted scorecard comment on PR #${pr}" \
      || echo "::warning::could not post the scorecard comment"
  fi
elif [ "$COMMENT" = "true" ]; then
  echo "==> not a pull-request event (or no token); skipping the sticky comment"
fi

# Job summary is always useful, PR or not.
{ echo "## IronClaw sandbox scan"; echo; cat "$scorecard"; } >>"${GITHUB_STEP_SUMMARY:-/dev/null}" 2>/dev/null || true

# ---------------------------------------------------------------------------
# 5. Gate: fail the check when below min-score (0 = report-only).
# ---------------------------------------------------------------------------
if [ "$MIN_SCORE" -gt 0 ] && [ "$score" -lt "$MIN_SCORE" ]; then
  out passed "false"
  die "containment score ${score}/100 is below the required ${MIN_SCORE}"
fi
out passed "true"
echo "==> scan passed (score ${score} >= min-score ${MIN_SCORE})"
