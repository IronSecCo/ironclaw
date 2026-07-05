#!/usr/bin/env bash
#
# containment-matrix.sh — a reproducible, head-to-head CONTAINMENT benchmark.
#
# sandbox-bench.sh next door measures *performance* (gVisor overhead). This harness
# measures the other axis IronClaw is sold on: *containment*. It runs one fixed
# escape-attempt suite from INSIDE several sandbox postures and records, per target,
# how many attacks the boundary blocks. The output is the results table the IRO-369
# comparison page is built from:
#
#   target | escape attempts | blocked | block rate | measured? | notes
#
# THREAT MODEL. Same as examples/red-team-escape (IRO-257/261/339): assume the agent
# is fully compromised and can run ARBITRARY CODE inside its sandbox. We do not test
# "can the model be tricked" — we test "WHEN it is, does the boundary hold." Each
# probe is a real attack primitive run as the container's own unprivileged process.
#
# WHAT IT MEASURES (runnable in CI on a Linux host with Docker; runsc optional):
#   - raw Docker (default runc) ...... an unhardened container: the common baseline.
#   - hardened Docker (runc) ......... network=none + all caps dropped + read-only +
#                                       no-new-privs = IronClaw's runc fallback posture.
#   - gVisor (runsc) ................. the same hardening on gVisor = IronClaw's
#                                       PRODUCTION runtime. A user-space kernel, so the
#                                       host kernel is never reached directly.
#
# WHAT IT DOES NOT MEASURE (sourced qualitatively on the docs page, never fabricated):
#   - Hosted sandboxes (E2B, Daytona) need vendor accounts / API keys and cannot run in
#     a secret-free CI job. The docs page describes their architecture from published
#     docs and labels those rows "sourced", with no invented numbers.
#
# Each attempt is scored OPEN (the attack succeeded — bad) or BLOCKED (contained — good)
# by OBSERVING real behavior, not by being told what target it is. Every target also has
# an EXPECTED posture; if what we observe diverges, the run prints a REGRESSION line and
# exits non-zero, exactly like the sandbox-containment gate. So this doubles as a gate:
# a change that weakens a runtime's posture turns it RED, it does not silently reprint a
# stale table.
#
# Usage:
#   scripts/bench/containment-matrix.sh [--out DIR] [--targets a,b,c] [--keep]
#   scripts/bench/containment-matrix.sh --self-test   # no Docker; validates the harness
#
# Env overrides:
#   MATRIX_IMAGE     probe image (busybox)         (default busybox:1.36.1)
#   MATRIX_RUNSC     runsc docker runtime name     (default runsc; skipped if absent)
#   MATRIX_EGRESS_IP an IP the box has no business reaching  (default 1.1.1.1)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

IMAGE="${MATRIX_IMAGE:-busybox:1.36.1}"
RUNSC_RUNTIME="${MATRIX_RUNSC:-runsc}"
EGRESS_IP="${MATRIX_EGRESS_IP:-1.1.1.1}"

OUT_DIR=""
KEEP=0
SELF_TEST=0
DRY_RUN=0
TARGET_FILTER=""
for ((i = 1; i <= $#; i++)); do
  case "${!i}" in
    --out)      j=$((i + 1)); OUT_DIR="${!j}"; i=$j ;;
    --targets)  j=$((i + 1)); TARGET_FILTER="${!j}"; i=$j ;;
    --keep)     KEEP=1 ;;
    --self-test) SELF_TEST=1 ;;
    --dry-run)  DRY_RUN=1 ;;
    -h | --help) sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: ${!i} (try --help)" >&2; exit 2 ;;
  esac
done

log() { printf '>> %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --------------------------------------------------------------------------- #
# The fixed escape-attempt suite (runs INSIDE the sandbox, portable busybox sh).
#
# Emits one line per attempt:  ATTEMPT|<id>|<OPEN|BLOCKED>|<detail>
# OPEN  = the attack primitive succeeded  (the boundary let it through — bad).
# BLOCKED = the primitive was contained   (good).
# The probe judges by OBSERVING behavior; it is never told which target it is.
# --------------------------------------------------------------------------- #
read -r -d '' PROBE <<'PROBE_EOF' || true
# 1) NETWORK EGRESS — the sandbox should have no NIC but loopback (network=none).
nics="$(ls -1 /sys/class/net 2>/dev/null | grep -v '^lo$' | tr '\n' ' ' | sed 's/ *$//')"
if [ -z "$nics" ]; then
  echo "ATTEMPT|net.nic|BLOCKED|only loopback present"
else
  echo "ATTEMPT|net.nic|OPEN|non-loopback NIC(s): $nics"
fi

# 2) NETWORK EGRESS (behavioural) — reach a public IP the box has no business reaching.
if wget -q -T 3 -O /dev/null "http://__EGRESS_IP__/" 2>/dev/null \
   || nc -w 3 "__EGRESS_IP__" 443 </dev/null >/dev/null 2>&1; then
  echo "ATTEMPT|net.egress|OPEN|outbound connection to __EGRESS_IP__ succeeded"
else
  echo "ATTEMPT|net.egress|BLOCKED|outbound connection to __EGRESS_IP__ failed"
fi

# 3) HOST FILESYSTEM — the host root fs must not be in the sandbox mount namespace.
#    Probe host-only markers (a bind of / would surface as /host, and the CI harness
#    can drop /etc/ironclaw-host-marker on the host to prove a real bind would leak).
if [ -e /host ] || [ -r /etc/ironclaw-host-marker ]; then
  echo "ATTEMPT|fs.host|OPEN|a host-only path was reachable"
else
  echo "ATTEMPT|fs.host|BLOCKED|host root not mounted"
fi

# 4) PRIVILEGED SYSCALL — mounting a filesystem needs CAP_SYS_ADMIN, which a hardened
#    sandbox drops. An unprivileged container (--privileged) would let this through.
mkdir -p /tmp/.mp 2>/dev/null || true
if mount -t tmpfs none /tmp/.mp >/dev/null 2>&1; then
  umount /tmp/.mp >/dev/null 2>&1 || true
  echo "ATTEMPT|caps.privileged|OPEN|mount(2) succeeded (CAP_SYS_ADMIN available)"
else
  echo "ATTEMPT|caps.privileged|BLOCKED|mount(2) denied (no CAP_SYS_ADMIN)"
fi

# 5) HOST KERNEL SURFACE — gVisor's differentiator. A shared-kernel runtime (runc)
#    reaches the real host kernel directly (its full syscall + CVE surface); gVisor
#    interposes a user-space kernel (Sentry), so /proc/version reports gVisor and the
#    host kernel is never the one serving the syscall.
ver="$(cat /proc/version 2>/dev/null || echo unknown)"
if echo "$ver" | grep -qi gvisor; then
  echo "ATTEMPT|kernel.shared-host|BLOCKED|syscalls served by gVisor Sentry, not the host kernel"
else
  short="$(echo "$ver" | cut -c1-48)"
  echo "ATTEMPT|kernel.shared-host|OPEN|host kernel reached directly: $short"
fi
PROBE_EOF
PROBE="${PROBE//__EGRESS_IP__/$EGRESS_IP}"

# The canonical attempt order (also the row order in the table).
ATTEMPT_IDS=(net.nic net.egress fs.host caps.privileged kernel.shared-host)
TOTAL_ATTEMPTS=${#ATTEMPT_IDS[@]}

# --------------------------------------------------------------------------- #
# Targets. Each is: key | label | measured|sourced | docker-run flags | notes
# The flags are what a user would actually type — the posture is the deployment.
# --------------------------------------------------------------------------- #
target_flags() {
  case "$1" in
    docker-default)  printf '%s' "" ;;
    docker-hardened) printf '%s' "--network=none --cap-drop=ALL --security-opt=no-new-privileges --read-only --tmpfs /tmp --user 65532:65532" ;;
    gvisor)          printf '%s' "--runtime=$RUNSC_RUNTIME --network=none --cap-drop=ALL --security-opt=no-new-privileges --read-only --tmpfs /tmp --user 65532:65532" ;;
    *) return 1 ;;
  esac
}
target_label() {
  case "$1" in
    docker-default)  printf '%s' "raw Docker (default runc)" ;;
    docker-hardened) printf '%s' "hardened Docker (runc)" ;;
    gvisor)          printf '%s' "gVisor / runsc (IronClaw runtime)" ;;
  esac
}
target_notes() {
  case "$1" in
    docker-default)  printf '%s' "unhardened container: bridge NIC, default caps, shared host kernel" ;;
    docker-hardened) printf '%s' "network=none + caps dropped + read-only; still shares the host kernel" ;;
    gvisor)          printf '%s' "IronClaw production posture: user-space kernel, host kernel never reached" ;;
  esac
}
# Expected BLOCKED attempts per target (space-separated). Divergence => REGRESSION.
target_expect_blocked() {
  case "$1" in
    docker-default)  printf '%s' "fs.host caps.privileged" ;;
    docker-hardened) printf '%s' "net.nic net.egress fs.host caps.privileged" ;;
    gvisor)          printf '%s' "net.nic net.egress fs.host caps.privileged kernel.shared-host" ;;
  esac
}

ALL_TARGETS=(docker-default docker-hardened gvisor)

# --------------------------------------------------------------------------- #
# Parse a probe's raw output into a "id=VERDICT" map echoed as lines; also count
# blocked. Sets globals via stdout the caller reads.
# --------------------------------------------------------------------------- #
# scores_from_output <raw>  -> prints "<id> <VERDICT> <detail>" per attempt (ordered);
# missing attempts are recorded as ERROR (probe never emitted them).
scores_from_output() {
  local raw="$1" id line verdict detail
  for id in "${ATTEMPT_IDS[@]}"; do
    line="$(printf '%s\n' "$raw" | grep "^ATTEMPT|$id|" | head -n1 || true)"
    if [ -z "$line" ]; then
      printf '%s ERROR probe-emitted-no-row\n' "$id"
      continue
    fi
    verdict="$(printf '%s' "$line" | cut -d'|' -f3)"
    detail="$(printf '%s' "$line" | cut -d'|' -f4-)"
    printf '%s %s %s\n' "$id" "$verdict" "$detail"
  done
}

# blocked_count <scores>  -> number of BLOCKED verdicts.
blocked_count() { printf '%s\n' "$1" | awk '$2=="BLOCKED"{n++} END{print n+0}'; }

# regression_check <target> <scores> -> prints REGRESSION lines; returns 1 if any.
regression_check() {
  local target="$1" scores="$2" rc=0 id verdict want expected got
  want=" $(target_expect_blocked "$target") "
  while read -r id verdict _; do
    [ -z "$id" ] && continue
    case "$want" in *" $id "*) expected=BLOCKED ;; *) expected=OPEN ;; esac
    got="$verdict"
    if [ "$got" != "$expected" ]; then
      printf 'REGRESSION|%s|%s|expected %s, observed %s\n' "$target" "$id" "$expected" "$got"
      rc=1
    fi
  done <<< "$scores"
  return $rc
}

# --------------------------------------------------------------------------- #
# Self-test: prove parsing / scoring / regression logic without any runtime.
# Runs anywhere (macOS included). Feeds synthetic probe output per target.
# --------------------------------------------------------------------------- #
synthetic_output() {
  case "$1" in
    docker-default)
      cat <<EOF
ATTEMPT|net.nic|OPEN|non-loopback NIC(s): eth0
ATTEMPT|net.egress|OPEN|outbound connection to $EGRESS_IP succeeded
ATTEMPT|fs.host|BLOCKED|host root not mounted
ATTEMPT|caps.privileged|BLOCKED|mount(2) denied (no CAP_SYS_ADMIN)
ATTEMPT|kernel.shared-host|OPEN|host kernel reached directly: Linux version 6.8.0
EOF
      ;;
    docker-hardened)
      cat <<EOF
ATTEMPT|net.nic|BLOCKED|only loopback present
ATTEMPT|net.egress|BLOCKED|outbound connection to $EGRESS_IP failed
ATTEMPT|fs.host|BLOCKED|host root not mounted
ATTEMPT|caps.privileged|BLOCKED|mount(2) denied (no CAP_SYS_ADMIN)
ATTEMPT|kernel.shared-host|OPEN|host kernel reached directly: Linux version 6.8.0
EOF
      ;;
    gvisor)
      cat <<EOF
ATTEMPT|net.nic|BLOCKED|only loopback present
ATTEMPT|net.egress|BLOCKED|outbound connection to $EGRESS_IP failed
ATTEMPT|fs.host|BLOCKED|host root not mounted
ATTEMPT|caps.privileged|BLOCKED|mount(2) denied (no CAP_SYS_ADMIN)
ATTEMPT|kernel.shared-host|BLOCKED|syscalls served by gVisor Sentry, not the host kernel
EOF
      ;;
  esac
}

# expected_blocked_total <target> -> the number of BLOCKED verdicts we expect.
expected_blocked_total() {
  case "$1" in
    docker-default)  echo 2 ;;
    docker-hardened) echo 4 ;;
    gvisor)          echo 5 ;;
    *) echo 0 ;;
  esac
}

run_self_test() {
  log "self-test: validating scoring + regression logic against synthetic probe output"
  local fails=0 target scores blocked want
  for target in "${ALL_TARGETS[@]}"; do
    scores="$(scores_from_output "$(synthetic_output "$target")")"
    blocked="$(blocked_count "$scores")"
    want="$(expected_blocked_total "$target")"
    if [ "$blocked" != "$want" ]; then
      printf 'FAIL: %s blocked=%s want=%s\n' "$target" "$blocked" "$want" >&2
      fails=$((fails + 1))
    fi
    if ! regression_check "$target" "$scores" >/dev/null; then
      printf 'FAIL: %s flagged a regression against its own expected posture\n' "$target" >&2
      fails=$((fails + 1))
    fi
  done
  # Negative control: a gVisor target that leaks the host kernel MUST be caught.
  local bad
  bad="$(scores_from_output "$(synthetic_output gvisor | sed 's#kernel.shared-host|BLOCKED.*#kernel.shared-host|OPEN|host kernel reached directly#')")"
  if regression_check gvisor "$bad" >/dev/null; then
    echo "FAIL: regression_check did NOT catch a gVisor kernel-surface leak" >&2
    fails=$((fails + 1))
  fi
  if [ "$fails" -eq 0 ]; then
    log "self-test PASSED (${#ALL_TARGETS[@]} targets + 1 negative control)"
    return 0
  fi
  die "self-test FAILED with $fails error(s)"
}

if [ "$SELF_TEST" -eq 1 ]; then
  run_self_test
  exit 0
fi

# --------------------------------------------------------------------------- #
# Live run
# --------------------------------------------------------------------------- #
# --dry-run: exercise the full render pipeline with synthetic probe output (no
# Docker). Used to validate artifact generation on hosts without a container runtime;
# never publish its numbers — they are the fixtures, not a measurement.
if [ "$DRY_RUN" -eq 0 ]; then
  command -v docker >/dev/null 2>&1 || die "this harness needs Docker (or run with --self-test / --dry-run)."
  docker info >/dev/null 2>&1 || die "Docker Engine is not responding."
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/ironclaw-cmatrix.XXXXXX")"
cleanup() { [ "$KEEP" -eq 1 ] || rm -rf "$WORKDIR"; }
trap cleanup EXIT
[ -z "$OUT_DIR" ] && OUT_DIR="$WORKDIR/results"
mkdir -p "$OUT_DIR"

# Decide the target set: honor --targets, else all; drop gvisor if runsc is not a
# registered docker runtime (so the harness still produces the runc rows honestly).
declare -a TARGETS
if [ -n "$TARGET_FILTER" ]; then
  IFS=',' read -r -a TARGETS <<< "$TARGET_FILTER"
else
  TARGETS=("${ALL_TARGETS[@]}")
fi
RUNSC_PRESENT=1
if [ "$DRY_RUN" -eq 0 ]; then
  if ! docker info --format '{{range .Runtimes}}{{.}} {{end}}' 2>/dev/null | grep -qw "$RUNSC_RUNTIME" \
     && ! docker info 2>/dev/null | grep -qiw "$RUNSC_RUNTIME"; then
    RUNSC_PRESENT=0
  fi
  log "pulling probe image $IMAGE"
  docker pull -q "$IMAGE" >/dev/null 2>&1 || die "could not pull $IMAGE"
fi

# Run each target, collect scores. Results are stashed in per-target files under
# $WORKDIR (bash 3.2 on macOS has no associative arrays; files keep it portable).
# has_scores <target> -> 0 if that target produced scores.
has_scores() { [ -f "$WORKDIR/scores-$1.txt" ]; }
get_scores() { cat "$WORKDIR/scores-$1.txt"; }
get_blocked() { cat "$WORKDIR/blocked-$1.txt"; }

GATE_FAILED=0
SKIPPED_NOTES=()
for target in "${TARGETS[@]}"; do
  if [ "$target" = "gvisor" ] && [ "$RUNSC_PRESENT" -eq 0 ]; then
    log "SKIP gvisor: runsc runtime '$RUNSC_RUNTIME' is not registered with Docker"
    SKIPPED_NOTES+=("gvisor: runsc not registered as a Docker runtime on this host")
    continue
  fi
  flags="$(target_flags "$target")" || die "unknown target: $target"
  log "[$target] running the escape suite ($(target_label "$target"))"
  if [ "$DRY_RUN" -eq 1 ]; then
    raw="$(synthetic_output "$target")"
  else
    # shellcheck disable=SC2086
    raw="$(docker run --rm $flags "$IMAGE" sh -c "$PROBE" 2>&1 || true)"
  fi
  scores="$(scores_from_output "$raw")"
  printf '%s\n' "$scores" > "$WORKDIR/scores-$target.txt"
  blocked_count "$scores" > "$WORKDIR/blocked-$target.txt"
  if reg="$(regression_check "$target" "$scores")"; [ -n "$reg" ]; then
    printf '%s\n' "$reg" >&2
    GATE_FAILED=1
  fi
done

# --------------------------------------------------------------------------- #
# Render: methodology.txt, results.json, results.md
# --------------------------------------------------------------------------- #
KERNEL="$(uname -sr)"
DOCKER_VER="$( (docker version --format '{{.Server.Version}}' 2>/dev/null || true) | head -n1 | tr -d '\r')"
[ -z "$DOCKER_VER" ] && DOCKER_VER="unknown"
RUNSC_VER="$( (runsc --version 2>/dev/null || true) | head -n1 | tr -d '\r')"
[ -z "$RUNSC_VER" ] && RUNSC_VER="not installed"
COMMIT="${GITHUB_SHA:-$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)}"

{
  echo "IronClaw containment matrix — methodology"
  echo "commit:        $COMMIT"
  echo "host kernel:   $KERNEL"
  echo "docker:        $DOCKER_VER"
  echo "runsc:         $RUNSC_VER"
  echo "probe image:   $IMAGE"
  echo "attempts:      ${ATTEMPT_IDS[*]}"
  echo "egress probe:  $EGRESS_IP"
} > "$OUT_DIR/methodology.txt"

# JSON
{
  printf '{\n'
  printf '  "commit": "%s",\n' "$COMMIT"
  printf '  "kernel": "%s",\n' "$KERNEL"
  printf '  "docker": "%s",\n' "$DOCKER_VER"
  printf '  "runsc": "%s",\n' "$RUNSC_VER"
  printf '  "total_attempts": %s,\n' "$TOTAL_ATTEMPTS"
  printf '  "targets": [\n'
  first=1
  for target in "${TARGETS[@]}"; do
    has_scores "$target" || continue
    [ "$first" -eq 0 ] && printf ',\n'
    first=0
    printf '    { "key": "%s", "label": "%s", "measured": true, "blocked": %s, "total": %s, "attempts": [' \
      "$target" "$(target_label "$target")" "$(get_blocked "$target")" "$TOTAL_ATTEMPTS"
    afirst=1
    while read -r id verdict detail; do
      [ -z "$id" ] && continue
      [ "$afirst" -eq 0 ] && printf ', '
      afirst=0
      printf '{ "id": "%s", "verdict": "%s" }' "$id" "$verdict"
    done <<< "$(get_scores "$target")"
    printf '] }'
  done
  printf '\n  ]\n}\n'
} > "$OUT_DIR/results.json"

# Markdown results table (the artifact the docs page mirrors).
{
  echo "| Target | Escape attempts | Blocked | Block rate | Measured | Notes |"
  echo "| --- | ---: | ---: | ---: | :---: | --- |"
  for target in "${TARGETS[@]}"; do
    has_scores "$target" || continue
    b="$(get_blocked "$target")"
    rate="$(awk "BEGIN{printf \"%d%%\", ($b/$TOTAL_ATTEMPTS)*100}")"
    printf '| %s | %s | %s | %s | yes | %s |\n' \
      "$(target_label "$target")" "$TOTAL_ATTEMPTS" "$b" "$rate" "$(target_notes "$target")"
  done
} > "$OUT_DIR/results.md"

# Per-attempt detail table (for the artifact / triage).
{
  echo
  echo "Per-attempt verdicts (OPEN = attack succeeded, BLOCKED = contained):"
  echo
  printf '%-22s' "attempt"
  for target in "${TARGETS[@]}"; do has_scores "$target" && printf '%-20s' "$target"; done
  echo
  for id in "${ATTEMPT_IDS[@]}"; do
    printf '%-22s' "$id"
    for target in "${TARGETS[@]}"; do
      has_scores "$target" || continue
      v="$(get_scores "$target" | awk -v i="$id" '$1==i{print $2}')"
      printf '%-20s' "$v"
    done
    echo
  done
} >> "$OUT_DIR/results.md"

echo
cat "$OUT_DIR/results.md"
echo
log "wrote $OUT_DIR/{results.md,results.json,methodology.txt}"
if [ "${#SKIPPED_NOTES[@]}" -gt 0 ]; then
  for n in "${SKIPPED_NOTES[@]}"; do log "note: skipped $n"; done
fi

if [ "$GATE_FAILED" -eq 1 ]; then
  echo
  die "a target's observed posture diverged from its expected containment matrix (see REGRESSION above)."
fi
log "all measured targets matched their expected containment posture."
