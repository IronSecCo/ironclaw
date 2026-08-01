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
# What a re-run does and does not reproduce:
#   * Image refs are mostly NOT pinned: only 16 of the 295 scenario rows in
#     images.txt carry an explicit @sha256 digest (12 of the 252 images that
#     become scorecards). The other 279 name a tag and resolve to whatever that
#     tag points at when the survey runs, so a fresh run tracks the tags as they
#     are published that day and is NOT guaranteed to reproduce the previously
#     published scores — by design, since the dataset is about what the ecosystem
#     ships today.
#   * What IS pinned is each published run's own provenance: for every scenario
#     results.json records, `.scenarios[].resolvedDigest` holds the registry
#     manifest digest actually scanned (non-empty on all 256 recorded
#     scenarios; it is an image-index digest for a multi-arch repo). So a
#     published score can be re-checked by pulling that digest and re-running the
#     row's flags — a manual re-scan from the recorded digests, which is a weaker
#     guarantee than a fresh run reproducing the scores, and not the same thing.
#   * Rows whose pull, run or scan fails are still SKIPPED rather than fatal (one
#     unavailable image must not wedge a 295-row sweep), but they are no longer
#     invisible: each is recorded with its stage and reason in `.skipped[]` of
#     results.json and listed in results.md, and coverage_guard.py fails the run
#     both when a row that scored in the last COMMITTED results.json stops
#     producing output and when the artifact's own arithmetic does not close
#     (scenarioCount + skippedCount == manifestRowCount). Baselining off the
#     committed copy rather than the working tree is what stops a plain re-run
#     from clearing a real regression (IRO-727). Before all this, the same 39
#     rows failed every weekly refresh and the run still exited green.
#   * The scan is read-only config inspection (docker inspect); it never runs the
#     image's real workload — the entrypoint is overridden with `sleep` purely to
#     keep the container alive for inspection.
#   * Rows in results.md are sorted by score, so a re-run diffs as score movement
#     rather than row churn (modulo the tool-version / timestamp stamp).
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

# Disk hygiene for large surveys. Scanning ~150 images pulls many GB; a CI runner
# (or a laptop VM) can run out of space long before the survey finishes. With
# PRUNE=1 each image we PULLED this run is removed right after it is scanned, so
# peak disk stays ~one image, not the whole set. Images already present locally
# before the run are left untouched. Off by default so cache-friendly local
# re-runs stay fast; the weekly refresh Action turns it on.
PRUNE="${PRUNE:-0}"

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
# git is a hard dependency, not an optional nicety: the coverage baseline is read
# out of HEAD (see below), so a missing git used to degrade silently into "no
# baseline committed" and an exit-0 run with the regression check switched off.
command -v git >/dev/null 2>&1 || die "git not found (needed to read the coverage baseline out of HEAD)"

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
  rm -f "${SKIPS:-}" "${SKIPS:+$SKIPS.tmp}" "${BASELINE:-}" \
        "${RECORDS:-}" "${RECORDS:+$RECORDS.tmp}" 2>/dev/null || true
  [ -n "${ERRDIR:-}" ] && rm -rf "$ERRDIR" 2>/dev/null
  [ "$KEEP" -eq 1 ] && { log "--keep: leaving ${#CREATED[@]} container(s) running"; return; }
  for c in "${CREATED[@]:-}"; do
    [ -n "$c" ] && "$DOCKER" rm -f "$c" >/dev/null 2>&1 || true
  done
  return 0
}
trap cleanup EXIT

# Per-scenario stderr captures. These used to be written into the invoking CWD
# as pull.err / run.err / scan.err / parse.err and were only removed on the
# happy path, so an aborted run left them behind in whatever directory you
# happened to be standing in.
ERRDIR="$(mktemp -d)"

# Accumulate per-scenario records as a JSON array on a temp file. Removed by
# cleanup(); the `trap ... RETURN` that used to do it is a no-op at top level in
# bash, so every run leaked this file.
RECORDS="$(mktemp)"
echo "[]" > "$RECORDS"

# …and every scenario we DROP, the same way: {label, image, stage, reason}. A
# skip used to be a log line in an Actions run that expires after 90 days, which
# is why 39 rows could fail every week without leaving a trace in the artifact
# (IRO-727). render.py folds this into results.json/.md.
SKIPS="$(mktemp)"
echo "[]" > "$SKIPS"

# The baseline coverage_guard.py compares against: the last COMMITTED
# results.json, read out of git — NOT the working-tree copy this run is about to
# overwrite. Snapshotting the working tree lets a plain re-run launder a
# regression green: run N writes the degraded results.json, run N+1 reads it
# back as its own baseline and sees no loss. Only a run whose guard passed ever
# gets committed, so HEAD is by construction a coverage level we actually held,
# and re-running a broken sweep fails again with the same message.
#
# No committed copy (a first run, a checkout without one, an export with no
# .git) means no baseline at all rather than a fallback to disk — the guard says
# the regression check was skipped instead of pretending it ran.
#
# "Nothing is committed yet" and "git failed" are NOT the same state and must not
# collapse into the same exit-0 message. Swallowing git's stderr made a broken
# object store — the ordinary result of running this in a container over a
# bind-mounted checkout owned by another uid — print "none committed" and run
# with the only real safety gate silently disabled. So exactly three states are
# a legitimate no-baseline, each established by a command that reads only refs
# or the index, never the object store; anything else is fatal.
BASELINE=""
BASELINE_PATH=""
if git_prefix="$(git -C "$SCRIPT_DIR" rev-parse --show-prefix 2>/dev/null)"; then
  BASELINE_PATH="${git_prefix}results.json"
  GIT_ERR="$ERRDIR/git.err"
  if head_ref="$(git -C "$SCRIPT_DIR" symbolic-ref -q HEAD 2>/dev/null)" \
     && ! git -C "$SCRIPT_DIR" show-ref --verify --quiet "$head_ref"; then
    # (1) A branch pointing at no commit: a fresh `git init`. show-ref reads
    # refs only, so this cannot be confused with an unreadable .git/objects.
    log "baseline: none committed — $head_ref has no commits yet, the coverage regression check will be skipped"
  elif ! listing="$(git -C "$SCRIPT_DIR" ls-tree --full-tree --name-only HEAD -- "$BASELINE_PATH" 2>"$GIT_ERR")"; then
    # (3) Anything else git has to say is a broken read, not an absent file.
    die "git could not read HEAD in $SCRIPT_DIR: $(head -1 "$GIT_ERR" 2>/dev/null). The coverage baseline is read from HEAD and is the only check standing between a permanently lost scenario and a green run, so a git failure fails the survey rather than downgrading it to an unchecked run"
  elif [ -z "$listing" ]; then
    # (2) git read HEAD fine and the file is genuinely not in it.
    log "baseline: none committed — $BASELINE_PATH is not in HEAD, the coverage regression check will be skipped"
  else
    BASELINE="$(mktemp)"
    git -C "$SCRIPT_DIR" show "HEAD:$BASELINE_PATH" > "$BASELINE" 2>"$GIT_ERR" \
      || die "git could not read HEAD:$BASELINE_PATH although ls-tree lists it: $(head -1 "$GIT_ERR" 2>/dev/null)"
    log "baseline: git HEAD:$BASELINE_PATH"
  fi
else
  log "baseline: none committed — $SCRIPT_DIR is not a git checkout, the coverage regression check will be skipped"
fi


# Rows whose record could not be written at all. Not a counter for show: an
# unwritten row is neither in .scenarios[] nor in .skipped[], so it lands as an
# unaccounted row and coverage_guard.py fails the run — with the artifact
# written, which is the point.
RECORD_FAILURES=0

# Collapse runs of blanks and trim, mirroring coverage_guard.manifest_labels.
#
# This used to be `echo "$x" | xargs`, which is a different function and an
# abort risk. xargs applies shell quote and backslash processing, so an
# unbalanced quote anywhere in images.txt is `xargs: unterminated quote`, a
# non-zero exit, and — under `set -e`, mid-sweep, before render — the death of
# every skip record collected so far. With no utility argument it also runs
# /bin/echo, which eats a leading `-n` or `-e` as an option rather than as data.
# `read -r -a` under the default IFS splits on exactly space/tab/newline and
# does no quote processing at all, so it cannot fail and the guard can mirror it
# exactly.
collapse() {
  local IFS=$' \t\n'
  local -a words=()
  read -r -a words <<<"${1-}" || true
  printf '%s' "${words[*]:-}"
}

# label image stage reason -> one entry in $SKIPS, plus the same line on stderr
# the script always logged. `stage` is one of pull|run|scan.
#
# This can never fail the run. It is called from four places under `set -euo
# pipefail` and it writes to a temp filesystem that PRUNE=1 exists precisely
# because it fills up: an ENOSPC used to return 1, abort the sweep and take
# cleanup() with it, deleting every skip recorded so far — destroying the record
# of exactly the failure worth recording. The write goes through a temp + rename
# so a failure leaves the previous content intact rather than a truncated file
# render.py cannot parse.
record_skip() {
  local label="$1" image="$2" stage="$3" reason="$4"
  log "[$n] $label — SKIP: $stage failed ($reason)"
  if ! python3 - "$SKIPS" "$label" "$image" "$stage" "$reason" <<'PY'
import json, os, sys
skipfile, label, image, stage, reason = sys.argv[1:6]
with open(skipfile) as f:
    skips = json.load(f)
skips.append({"label": label, "image": image, "stage": stage,
              "reason": reason.strip()})
tmp = skipfile + ".tmp"
with open(tmp, "w") as f:
    json.dump(skips, f)
os.replace(tmp, skipfile)
PY
  then
    RECORD_FAILURES=$((RECORD_FAILURES+1))
    log "[$n] $label — WARNING: the skip itself could not be recorded (temp filesystem full?). The row will be reported as unaccounted for and the coverage guard will fail this run."
  fi
  return 0
}

# Returns non-zero if the record could not be written, so the caller can decline
# to count the row as scanned. Called from an `if`, so `set -e` does not fire.
append_record() { # label image runFlags resolvedDigest scanjson-file
  local label="$1" image="$2" flags="$3" digest="$4" scanfile="$5"
  python3 - "$RECORDS" "$label" "$image" "$flags" "$digest" "$scanfile" <<'PY'
import json, os, sys
recfile, label, image, flags, digest, scanfile = sys.argv[1:7]
with open(recfile) as f:
    recs = json.load(f)
with open(scanfile) as f:
    report = json.load(f)
recs.append({"label": label, "image": image, "runFlags": flags,
             "resolvedDigest": digest, "report": report})
tmp = recfile + ".tmp"
with open(tmp, "w") as f:
    json.dump(recs, f)
os.replace(tmp, recfile)
PY
}

n=0
scanned=0
# `|| [ -n "$line" ]` so a manifest whose last line has no trailing newline is
# still swept. Without it the final row is silently dropped, and a silently
# dropped row is the whole bug (IRO-727) — coverage_guard.py parses the manifest
# independently and fails the run if its row count ever disagrees with $n.
while IFS= read -r line || [ -n "$line" ]; do
  # strip comments / blanks
  line="${line%%$'\r'}"
  case "$line" in ''|'#'*) continue;; esac
  # split on '|'
  IFS='|' read -r label image flags <<<"$line"
  label="$(collapse "$label")"
  image="$(collapse "$image")"
  flags="$(collapse "${flags:-}")"
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
  pulled=0
  if "$DOCKER" image inspect "$runref" >/dev/null 2>&1; then
    log "[$n] $label — cached $runref"
  else
    pulled=1
    log "[$n] $label — pulling $runref"
    tries=0
    until "$DOCKER" pull -q "$runref" >/dev/null 2>"$ERRDIR/pull.err"; do
      tries=$((tries+1))
      if grep -qi "rate limit" "$ERRDIR/pull.err" && [ "$tries" -lt 5 ]; then
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
      record_skip "$label" "$image" "pull" "$(head -1 "$ERRDIR/pull.err")"
      runref=""
      break
    done
    [ -z "$runref" ] && continue
  fi

  "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
  log "[$n] $label — docker run $flags --entrypoint sleep <image>"
  # shellcheck disable=SC2086
  if ! "$DOCKER" run -d --name "$cname" $flags --entrypoint sleep "$runref" 86400 >/dev/null 2>"$ERRDIR/run.err"; then
    record_skip "$label" "$image" "run" "$(head -1 "$ERRDIR/run.err")"
    continue
  fi
  CREATED+=("$cname")

  # The exact bits we scanned, by manifest digest — recorded for provenance so a
  # scorecard page always names the digest it graded (RepoDigests is empty only
  # for locally-built images, never for a pulled one).
  digest="$("$DOCKER" image inspect "$runref" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  digest="${digest#*@}"

  # A temp file we cannot even create is a SKIP too: `set -e` on the assignment
  # would abort the sweep here, before render, and take every skip with it.
  if ! scanfile="$(mktemp 2>"$ERRDIR/mktemp.err")"; then
    record_skip "$label" "$image" "scan" "could not create a temp file: $(head -1 "$ERRDIR/mktemp.err")"
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    continue
  fi
  if ! "$IRONCTL" scan "$cname" --json > "$scanfile" 2>"$ERRDIR/scan.err"; then
    record_skip "$label" "$image" "scan" "$(head -1 "$ERRDIR/scan.err")"; rm -f "$scanfile"
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    continue
  fi
  # A report we cannot parse is a SKIP, not a `set -e` abort. An abort here kills
  # the run before render.py writes anything, and cleanup() then deletes $SKIPS,
  # so every skip recorded up to that point is destroyed — the artifact has to
  # outlive the run for any of this to be worth anything (IRO-727).
  if ! summary="$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(d["score"],d["grade"])' "$scanfile" 2>"$ERRDIR/parse.err")"; then
    record_skip "$label" "$image" "scan" "unreadable scan report: $(head -1 "$ERRDIR/parse.err")"
    rm -f "$scanfile"
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    continue
  fi
  score="${summary%% *}"
  grade="${summary##* }"
  log "[$n] $label — ${score}/100 grade ${grade}"
  # Counted as scanned only if the record actually landed. A write that failed
  # must not inflate $scanned — the row is then unaccounted for, which is what
  # the coverage guard is for.
  if append_record "$label" "$image" "$flags" "$digest" "$scanfile"; then
    scanned=$((scanned+1))
  else
    RECORD_FAILURES=$((RECORD_FAILURES+1))
    log "[$n] $label — WARNING: scanned ${score}/100 but the result could not be recorded (temp filesystem full?). The row will be reported as unaccounted for and the coverage guard will fail this run."
  fi
  rm -f "$scanfile"

  if [ "$KEEP" -eq 0 ]; then
    "$DOCKER" rm -f "$cname" >/dev/null 2>&1 || true
    CREATED=("${CREATED[@]/$cname}")
    # Bound peak disk on a big survey: drop the image we just pulled + scanned.
    # Only images pulled THIS run are removed (pre-existing cache is preserved).
    if [ "$PRUNE" = "1" ] && [ "$pulled" = "1" ] && [ -n "$runref" ]; then
      "$DOCKER" rmi "$runref" >/dev/null 2>&1 || true
    fi
  fi
done < "$MANIFEST"

log "scanned ${scanned}/${n} scenarios"
[ "$RECORD_FAILURES" -eq 0 ] || \
  log "WARNING: $RECORD_FAILURES row(s) could not be recorded at all; they will show up as unaccounted for and fail the coverage guard below"

# Render FIRST — before the `scanned > 0` check and before the coverage guard,
# both deliberately. Whatever the sweep found is the evidence, and the whole
# point of IRO-727 is that it must outlive an Actions log that expires. The
# worst runs are the ones that most need explaining: a dead daemon or a mirror
# outage fails every row, and the old ordering died right here without writing
# anything, leaving the previous healthy results.json byte-identical on disk and
# cleanup() deleting $SKIPS on the way out. Now a run where every row failed
# leaves a results.json naming all $n rows and why each one dropped.
#
# What this ordering does NOT buy, stated precisely because a false durability
# claim is the same class of bug as the one being fixed: anything that aborts
# the sweep BEFORE this line still writes nothing and still loses every skip
# collected so far. The foreseeable internal ones are gone — a failed skip write
# on a full temp filesystem, a manifest row that blows up the field parse, and a
# temp file that cannot be created are all recorded and stepped over rather than
# fatal (which is why PRUNE=1, whose whole reason to exist is a nearly-full
# runner disk, is safe here). What remains is the process being killed (Ctrl-C,
# OOM, a job timeout), a manifest that cannot be read at all, and render.py
# itself failing. For the CI case, scores-refresh.yml uploads results.json and
# results.md as a run artifact with `if: always()` so a failing run's evidence
# outlives the runner rather than dying with it.
log "rendering results.json + results.md (${scanned} scenarios)…"
python3 "$SCRIPT_DIR/render.py" "$RESULTS_JSON" "$RESULTS_MD" \
  --skips "$SKIPS" --manifest-rows "$n" < "$RECORDS"

log "done: $RESULTS_JSON"
log "done: $RESULTS_MD"

[ "${scanned:-0}" -gt 0 ] || die "no scenarios scanned from $MANIFEST — results.* were still written and list every dropped row"

# Coverage regression check: a scenario that scored last run and is still in the
# manifest must score again. Transient registry weather is absent from the
# baseline too, so it cannot trip this; a row rotting for good does.
guard_args=(--manifest "$MANIFEST" --results "$RESULTS_JSON")
[ -n "$BASELINE" ] && guard_args+=(--baseline "$BASELINE")
python3 "$SCRIPT_DIR/coverage_guard.py" "${guard_args[@]}" \
  || die "coverage regressed — results.* were still written, see .skipped[]"
