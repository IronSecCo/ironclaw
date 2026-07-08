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

die() { echo "error: $*" >&2; exit 1; }
log() { echo ">> $*" >&2; }

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

append_record() { # label image runFlags scanjson-file
  local label="$1" image="$2" flags="$3" scanfile="$4"
  python3 - "$RECORDS" "$label" "$image" "$flags" "$scanfile" <<'PY'
import json, sys
recfile, label, image, flags, scanfile = sys.argv[1:6]
with open(recfile) as f:
    recs = json.load(f)
with open(scanfile) as f:
    report = json.load(f)
recs.append({"label": label, "image": image, "runFlags": flags, "report": report})
with open(recfile, "w") as f:
    json.dump(recs, f)
PY
}

n=0
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

  # Pull only if the pinned digest is not already present locally. This makes
  # re-runs fast and lets the survey complete from cache even when Docker Hub's
  # anonymous pull-rate limit is exhausted. On a 429, back off and retry a few
  # times; if you hit this often, `docker login` (a free account) lifts the
  # anonymous limit — see README.md.
  if "$DOCKER" image inspect "$image" >/dev/null 2>&1; then
    log "[$n] $label — cached $image"
  else
    log "[$n] $label — pulling $image"
    tries=0
    until "$DOCKER" pull -q "$image" >/dev/null 2>pull.err; do
      tries=$((tries+1))
      if grep -qi "rate limit" pull.err && [ "$tries" -lt 5 ]; then
        wait=$((tries*30))
        log "[$n] $label — rate limited, retrying in ${wait}s (try $tries/5)"
        sleep "$wait"
        continue
      fi
      cat pull.err >&2; rm -f pull.err
      die "pull failed for $image (see above; 'docker login' lifts anon rate limits)"
    done
    rm -f pull.err
  fi

  "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
  log "[$n] $label — docker run $flags --entrypoint sleep <image>"
  # shellcheck disable=SC2086
  "$DOCKER" run -d --name "$cname" $flags --entrypoint sleep "$image" 86400 >/dev/null
  CREATED+=("$cname")

  scanfile="$(mktemp)"
  "$IRONCTL" scan "$cname" --json > "$scanfile"
  score="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["score"])' "$scanfile")"
  grade="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["grade"])' "$scanfile")"
  log "[$n] $label — ${score}/100 grade ${grade}"
  append_record "$label" "$image" "$flags" "$scanfile"
  rm -f "$scanfile"

  if [ "$KEEP" -eq 0 ]; then
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    CREATED=("${CREATED[@]/$cname}")
  fi
done < "$MANIFEST"

[ "$n" -gt 0 ] || die "no scenarios found in $MANIFEST"

log "rendering results.json + results.md ($n scenarios)…"
python3 "$SCRIPT_DIR/render.py" "$RESULTS_JSON" "$RESULTS_MD" < "$RECORDS"

log "done: $RESULTS_JSON"
log "done: $RESULTS_MD"
