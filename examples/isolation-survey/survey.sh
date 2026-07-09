#!/usr/bin/env bash
# isolation-survey — run `ironctl scan` over a curated, pinned set of popular
# PUBLIC container images and their common run configurations, and emit a
# combined results.json + a rendered results.md table.
#
# This is the reproducible harness behind the "State of Container Isolation"
# dataset (IRO-436): it turns `ironctl scan` from a per-user tool into a
# defensible, shareable artifact. No credentials, no cloud, no account — only a
# working Docker daemon (plus Go to build ironctl, and python3 to render, both
# standard on any dev box / CI runner).
#
#   examples/isolation-survey/survey.sh              # scan every scenario, write results.*
#   examples/isolation-survey/survey.sh --keep       # leave containers running afterwards
#   IRONCTL=/path/to/ironctl examples/isolation-survey/survey.sh   # use a prebuilt binary
#
# Determinism / reproducibility:
#   * Every image is pinned by its multi-arch manifest-list digest in images.txt,
#     so `docker pull` resolves identical bits on amd64 and arm64.
#   * The scan is read-only config inspection (docker inspect); it never runs the
#     image's real workload — the entrypoint is overridden with `sleep` purely to
#     keep the container alive for inspection.
#   * Rows in results.md are sorted by score, so a re-run is byte-identical modulo
#     the tool-version / timestamp stamp.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANIFEST="$SCRIPT_DIR/images.txt"
RESULTS_JSON="$SCRIPT_DIR/results.json"
RESULTS_MD="$SCRIPT_DIR/results.md"
NAME_PREFIX="ic-survey-"

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

DOCKER="${DOCKER:-docker}"

# Mirror-first pulls. Docker Hub's anonymous pull-rate limit (100 pulls / 6h per
# IP) is the single biggest cause of a partial survey once the image set grows
# past a couple dozen. mirror.gcr.io is Google's pull-through cache for Docker
# Hub and is NOT anonymously rate-limited, so we resolve every image through it
# by default (the bits are identical — both registries are content-addressed).
# Set MIRROR=0 to pull straight from the original ref instead.
MIRROR="${MIRROR:-1}"
MIRROR_HOST="${MIRROR_HOST:-mirror.gcr.io}"

die() { echo "error: $*" >&2; exit 1; }
log() { echo ">> $*" >&2; }

# mirror_ref <repo[:tag][@digest]> -> the same image on $MIRROR_HOST.
# Official (single-segment) repos are namespaced under library/; already-
# namespaced repos (grafana/grafana, prom/prometheus, hashicorp/vault) pass
# through unchanged. A pinned @sha256 digest is preserved; the digest is the
# manifest digest and is identical across registries.
mirror_ref() {
  local ref="$1" repo digest="" path reponame
  repo="${ref%%@*}"
  [ "$ref" != "$repo" ] && digest="${ref#*@}"
  case "$repo" in
    */*) path="$repo" ;;                 # already namespaced
    *)   path="library/$repo" ;;         # official image
  esac
  if [ -n "$digest" ]; then
    reponame="${path%%:*}"               # drop :tag for a digest pull
    echo "${MIRROR_HOST}/${reponame}@${digest}"
  else
    echo "${MIRROR_HOST}/${path}"
  fi
}

command -v "$DOCKER" >/dev/null 2>&1 || die "docker not found (set \$DOCKER)"
"$DOCKER" info >/dev/null 2>&1 || die "docker daemon not reachable (is it running?)"
command -v python3 >/dev/null 2>&1 || die "python3 not found (needed to render results)"

# Resolve ironctl: prefer $IRONCTL, else a repo-local build, else build it.
IRONCTL="${IRONCTL:-}"
if [ -z "$IRONCTL" ]; then
  if [ -x "$REPO_ROOT/bin/ironctl" ]; then
    IRONCTL="$REPO_ROOT/bin/ironctl"
  else
    log "building ironctl (CGO_ENABLED=1)…"
    ( cd "$REPO_ROOT" && CGO_ENABLED=1 go build -o "$REPO_ROOT/bin/ironctl" ./cmd/ironctl )
    IRONCTL="$REPO_ROOT/bin/ironctl"
  fi
fi
[ -x "$IRONCTL" ] || die "ironctl not executable: $IRONCTL"
log "ironctl: $IRONCTL ($("$IRONCTL" scan --help >/dev/null 2>&1 && echo ok))"

# Track containers we create so we can always tear them down.
CREATED=()
cleanup() {
  [ "$KEEP" -eq 1 ] && { log "--keep: leaving ${#CREATED[@]} container(s) running"; return; }
  for c in "${CREATED[@]:-}"; do
    [ -n "$c" ] && "$DOCKER" rm -f "$c" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

# Accumulate per-scenario records as a JSON array on a temp file.
RECORDS="$(mktemp)"
trap 'rm -f "$RECORDS"' RETURN 2>/dev/null || true
echo "[]" > "$RECORDS"

append_record() { # label image runFlags resolvedDigest scanjson-file
  local label="$1" image="$2" flags="$3" digest="$4" scanfile="$5"
  python3 - "$RECORDS" "$label" "$image" "$flags" "$digest" "$scanfile" <<'PY'
import json, sys
recfile, label, image, flags, digest, scanfile = sys.argv[1:7]
with open(recfile) as f:
    recs = json.load(f)
with open(scanfile) as f:
    report = json.load(f)
recs.append({"label": label, "image": image, "runFlags": flags,
             "resolvedDigest": digest, "report": report})
with open(recfile, "w") as f:
    json.dump(recs, f)
PY
}

n=0
scanned=0
while IFS= read -r line; do
  # strip comments / blanks
  line="${line%%$'\r'}"
  case "$line" in ''|'#'*) continue;; esac
  # split on '|'
  IFS='|' read -r label image flags <<<"$line"
  label="$(echo "$label" | xargs)"
  image="$(echo "$image" | xargs)"
  flags="$(echo "${flags:-}" | xargs || true)"
  [ -z "$label" ] && continue
  n=$((n+1))
  cname="${NAME_PREFIX}${label}"

  # Resolve the local ref to run: mirror.gcr.io by default (no anon rate limit),
  # falling back to the original ref if the mirror does not have the image.
  if [ "$MIRROR" = "1" ]; then
    runref="$(mirror_ref "$image")"
  else
    runref="$image"
  fi

  # Pull only if not already present locally (fast, cache-friendly re-runs). On
  # a rate limit, back off and retry; if the mirror can't serve it, fall back to
  # the original ref once. A scenario that still can't be pulled is SKIPPED (not
  # fatal) so one unavailable image never aborts a 50-image survey.
  if "$DOCKER" image inspect "$runref" >/dev/null 2>&1; then
    log "[$n] $label — cached $runref"
  else
    log "[$n] $label — pulling $runref"
    tries=0
    until "$DOCKER" pull -q "$runref" >/dev/null 2>pull.err; do
      tries=$((tries+1))
      if grep -qi "rate limit" pull.err && [ "$tries" -lt 5 ]; then
        wait=$((tries*30))
        log "[$n] $label — rate limited, retrying in ${wait}s (try $tries/5)"
        sleep "$wait"
        continue
      fi
      if [ "$MIRROR" = "1" ] && [ "$runref" != "$image" ]; then
        log "[$n] $label — mirror miss, falling back to $image"
        runref="$image"
        continue
      fi
      log "[$n] $label — SKIP: pull failed ($(head -1 pull.err))"
      rm -f pull.err
      runref=""
      break
    done
    rm -f pull.err
    [ -z "$runref" ] && continue
  fi

  "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
  log "[$n] $label — docker run $flags --entrypoint sleep <image>"
  # shellcheck disable=SC2086
  if ! "$DOCKER" run -d --name "$cname" $flags --entrypoint sleep "$runref" 86400 >/dev/null 2>run.err; then
    log "[$n] $label — SKIP: docker run failed ($(head -1 run.err))"; rm -f run.err
    continue
  fi
  rm -f run.err
  CREATED+=("$cname")

  # The exact bits we scanned, by manifest digest — recorded for provenance so a
  # scorecard page always names the digest it graded (RepoDigests is empty only
  # for locally-built images, never for a pulled one).
  digest="$("$DOCKER" image inspect "$runref" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  digest="${digest#*@}"

  scanfile="$(mktemp)"
  if ! "$IRONCTL" scan "$cname" --json > "$scanfile" 2>scan.err; then
    log "[$n] $label — SKIP: scan failed ($(head -1 scan.err))"; rm -f scan.err "$scanfile"
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    continue
  fi
  rm -f scan.err
  score="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["score"])' "$scanfile")"
  grade="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["grade"])' "$scanfile")"
  log "[$n] $label — ${score}/100 grade ${grade}"
  append_record "$label" "$image" "$flags" "$digest" "$scanfile"
  rm -f "$scanfile"
  scanned=$((scanned+1))

  if [ "$KEEP" -eq 0 ]; then
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    CREATED=("${CREATED[@]/$cname}")
  fi
done < "$MANIFEST"

[ "${scanned:-0}" -gt 0 ] || die "no scenarios scanned from $MANIFEST"
log "scanned ${scanned}/${n} scenarios"

log "rendering results.json + results.md (${scanned} scenarios)…"
python3 "$SCRIPT_DIR/render.py" "$RESULTS_JSON" "$RESULTS_MD" < "$RECORDS"

log "done: $RESULTS_JSON"
log "done: $RESULTS_MD"
