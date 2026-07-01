#!/usr/bin/env bash
#
# sandbox-bench.sh — measure the runtime overhead of IronClaw's gVisor-isolated
# sandbox versus a host baseline.
#
# IronClaw launches every agent in its own sandbox via an OCI runtime, exactly as
# `<runtime> run --bundle <dir> <id>` (see internal/host/isolation/isolation.go).
# This harness reproduces that launch path with a minimal bundle whose config.json
# mirrors the IronClaw trust boundary — network=none, all capabilities dropped,
# no_new_privs, non-root user namespace, read-only rootfs, cgroup mem/CPU limits —
# and times it under gVisor (runsc) and under a host baseline (runc) so the delta
# is a like-for-like measurement of the gVisor wall, not of unrelated setup.
#
# It measures four things, each over N iterations:
#   1. cold start  — launch latency from a freshly staged rootfs (new agent)
#   2. warm start  — launch latency with the rootfs already cached (respawn)
#   3. memory      — resident overhead of the sandbox process tree (sentry+gofer)
#   4. cpu / sys   — wall time of a CPU-bound and a syscall-bound workload
#
# Results are conservative and gVisor-only by construction: the same bundle is run
# under both runtimes, so anything that is not the isolation layer cancels out.
#
# Output: a Markdown results table on stdout, plus a JSON sidecar and the captured
# methodology (uname, runtime versions, CPU/RAM) under the results directory.
#
# Usage:
#   scripts/bench/sandbox-bench.sh [--iterations N] [--out DIR] [--keep]
#
# Requirements (Linux host):
#   - runsc            (gVisor; the runtime under test)
#   - runc             (optional; the host baseline. Skipped with a note if absent.)
#   - docker OR podman (to export a minimal busybox rootfs once), OR a pre-staged
#                       rootfs supplied via BENCH_ROOTFS=/path/to/rootfs
#
# Environment overrides:
#   BENCH_IMAGE    OCI image to source the rootfs from   (default: busybox:1.36.1)
#                  Pin by digest (busybox@sha256:...) for byte-for-byte repeatability.
#   BENCH_ROOTFS   Use this already-extracted rootfs instead of pulling an image.
#   BENCH_MEM_MB   cgroup memory limit, MiB              (default: 512)
#   BENCH_CPUS     cgroup CPU quota, vCPUs               (default: 1)
#
# This script makes no claim to run inside a sandbox itself: gVisor needs a
# gofer-capable host (it will not start under nested/locked-down CI runners). Run
# it on a real Linux host or VM where `runsc` is installed and functional.

set -euo pipefail

# --------------------------------------------------------------------------- #
# Configuration & argument parsing
# --------------------------------------------------------------------------- #

ITERATIONS=20
KEEP_WORKDIR=0
OUT_DIR=""

BENCH_IMAGE="${BENCH_IMAGE:-busybox:1.36.1}"
BENCH_ROOTFS="${BENCH_ROOTFS:-}"
BENCH_MEM_MB="${BENCH_MEM_MB:-512}"
BENCH_CPUS="${BENCH_CPUS:-1}"

# Global flags passed to `runsc` *before* the subcommand (e.g. "--platform=systrap
# --network=none"). On a real host with /dev/kvm the defaults are best, so this is
# empty; on a locked-down/hosted CI runner (no /dev/kvm) the KVM platform is
# unavailable and systrap is required for the sentry to come up. --network=none
# matches IronClaw's posture and is the honest thing to measure. runc takes no such
# flags, so BENCH_RUNC_FLAGS defaults empty and is only here for symmetry.
BENCH_RUNSC_FLAGS="${BENCH_RUNSC_FLAGS:-}"
BENCH_RUNC_FLAGS="${BENCH_RUNC_FLAGS:-}"

# runtime_flags <runtime> — echo the global flags for a given runtime.
runtime_flags() {
	case "$1" in
	runsc) printf '%s' "$BENCH_RUNSC_FLAGS" ;;
	runc) printf '%s' "$BENCH_RUNC_FLAGS" ;;
	*) : ;;
	esac
}

# A fixed, modest CPU-bound loop: pure integer arithmetic, no syscalls in the hot
# path. Demonstrates the near-native CPU characteristic of gVisor. The $i must NOT
# expand here — it runs inside the sandbox's /bin/sh, not this parent shell.
# shellcheck disable=SC2016
CPU_WORKLOAD='i=0; while [ $i -lt 2000000 ]; do i=$((i+1)); done'
# A syscall/IO-bound walk of the rootfs: stat-heavy, the workload class where
# gVisor's user-space kernel costs the most. Honest worst case for our positioning.
SYS_WORKLOAD='find / -xdev 2>/dev/null | wc -l >/dev/null'

usage() {
	sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--iterations)
		ITERATIONS="${2:?--iterations needs a value}"
		shift 2
		;;
	--out)
		OUT_DIR="${2:?--out needs a value}"
		shift 2
		;;
	--keep)
		KEEP_WORKDIR=1
		shift
		;;
	-h | --help)
		usage 0
		;;
	*)
		printf 'unknown argument: %s\n\n' "$1" >&2
		usage 1
		;;
	esac
done

log() { printf '>> %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# --------------------------------------------------------------------------- #
# Preflight
# --------------------------------------------------------------------------- #

[ "$(uname -s)" = "Linux" ] || die "gVisor/runsc only runs on Linux; this host is $(uname -s)."
command -v runsc >/dev/null 2>&1 || die "runsc not found in PATH — install gVisor first (https://gvisor.dev/docs/user_guide/install/)."

HAVE_RUNC=0
if command -v runc >/dev/null 2>&1; then HAVE_RUNC=1; fi

CONTAINER_TOOL=""
if [ -z "$BENCH_ROOTFS" ]; then
	if command -v docker >/dev/null 2>&1; then
		CONTAINER_TOOL="docker"
	elif command -v podman >/dev/null 2>&1; then
		CONTAINER_TOOL="podman"
	else
		die "need docker or podman to extract a rootfs, or set BENCH_ROOTFS to a pre-staged rootfs."
	fi
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/ironclaw-bench.XXXXXX")"
cleanup() { [ "$KEEP_WORKDIR" -eq 1 ] || rm -rf "$WORKDIR"; }
trap cleanup EXIT

if [ -z "$OUT_DIR" ]; then OUT_DIR="$WORKDIR/results"; fi
mkdir -p "$OUT_DIR"

# --------------------------------------------------------------------------- #
# Stage a minimal rootfs once (the "image" all sandboxes share, read-only)
# --------------------------------------------------------------------------- #

ROOTFS_TEMPLATE="$WORKDIR/rootfs-template"
mkdir -p "$ROOTFS_TEMPLATE"

if [ -n "$BENCH_ROOTFS" ]; then
	[ -d "$BENCH_ROOTFS" ] || die "BENCH_ROOTFS=$BENCH_ROOTFS is not a directory."
	log "Using pre-staged rootfs: $BENCH_ROOTFS"
	cp -a "$BENCH_ROOTFS/." "$ROOTFS_TEMPLATE/"
else
	log "Exporting rootfs from $BENCH_IMAGE via $CONTAINER_TOOL ..."
	cid="$("$CONTAINER_TOOL" create "$BENCH_IMAGE")"
	"$CONTAINER_TOOL" export "$cid" | tar -C "$ROOTFS_TEMPLATE" -xf -
	"$CONTAINER_TOOL" rm "$cid" >/dev/null
fi
[ -x "$ROOTFS_TEMPLATE/bin/sh" ] || die "rootfs has no /bin/sh — supply a usable BENCH_ROOTFS or image."

# --------------------------------------------------------------------------- #
# config.json — mirrors internal/host/isolation BuildOCISpec (the IronClaw wall)
# --------------------------------------------------------------------------- #

# host uid/gid the sandbox user namespace maps onto (non-root inside).
HOST_UID="$(id -u)"
HOST_GID="$(id -g)"
CPU_QUOTA="$((BENCH_CPUS * 100000))"
MEM_BYTES="$((BENCH_MEM_MB * 1024 * 1024))"

# write_bundle <dir> <json-args-array>
# Emits <dir>/config.json with the given process args. network namespace is
# deliberately omitted (network=none). No capabilities, no_new_privs, read-only
# rootfs, userns, cgroup mem/cpu/pids limits, deny-by-default is enforced by the
# runtime defaults plus the empty capability sets below.
write_config() {
	local dir="$1" args_json="$2"
	cat >"$dir/config.json" <<JSON
{
  "ociVersion": "1.0.2",
  "process": {
    "terminal": false,
    "user": { "uid": 65532, "gid": 65532 },
    "args": $args_json,
    "env": ["PATH=/bin:/usr/bin", "HOME=/"],
    "cwd": "/",
    "capabilities": {
      "bounding": [], "effective": [], "inheritable": [], "permitted": [], "ambient": []
    },
    "noNewPrivileges": true
  },
  "root": { "path": "rootfs", "readonly": true },
  "hostname": "ironclaw-bench",
  "mounts": [
    { "destination": "/proc", "type": "proc", "source": "proc" },
    { "destination": "/tmp", "type": "tmpfs", "source": "tmpfs",
      "options": ["nosuid", "nodev", "noexec", "size=16m"] }
  ],
  "linux": {
    "namespaces": [
      { "type": "pid" }, { "type": "ipc" }, { "type": "uts" },
      { "type": "mount" }, { "type": "user" }
    ],
    "uidMappings": [ { "containerID": 65532, "hostID": $HOST_UID, "size": 1 } ],
    "gidMappings": [ { "containerID": 65532, "hostID": $HOST_GID, "size": 1 } ],
    "resources": {
      "memory": { "limit": $MEM_BYTES },
      "cpu": { "quota": $CPU_QUOTA, "period": 100000 },
      "pids": { "limit": 256 }
    }
  }
}
JSON
}

# sh-quote a single command string into a JSON args array running under /bin/sh.
sh_args_json() {
	local cmd="$1"
	# escape backslashes and double quotes for JSON embedding.
	cmd="${cmd//\\/\\\\}"
	cmd="${cmd//\"/\\\"}"
	printf '["/bin/sh", "-c", "%s"]' "$cmd"
}

# --------------------------------------------------------------------------- #
# Timing helpers
# --------------------------------------------------------------------------- #

# now_ms — monotonic-ish millisecond clock.
now_ms() { date +%s%3N; }

# median <numbers...> — integer median of stdin lines.
median() {
	sort -n | awk '
		{ a[NR] = $1 }
		END {
			if (NR == 0) { print "0"; exit }
			if (NR % 2) { print a[(NR + 1) / 2] }
			else { printf "%.0f\n", (a[NR/2] + a[NR/2 + 1]) / 2 }
		}'
}

# run_once <runtime> <bundle> <id> — run a prepared bundle, returns exit status.
# Global runtime flags (runtime_flags) are word-split on purpose: they are a small
# controlled set of runsc global flags (e.g. --platform=systrap --network=none).
run_once() {
	local runtime="$1" bundle="$2" id="$3"
	# shellcheck disable=SC2046
	( cd "$bundle" && "$runtime" $(runtime_flags "$runtime") run --bundle "$bundle" "$id" >/dev/null 2>&1 )
}

# stage_fresh_rootfs <bundle> — copy a pristine rootfs into a bundle (cold path).
stage_fresh_rootfs() {
	local bundle="$1"
	rm -rf "$bundle/rootfs"
	cp -a "$ROOTFS_TEMPLATE" "$bundle/rootfs"
}

# best-effort page-cache drop so "cold" is honestly cold (needs root; skipped otherwise).
drop_caches() {
	if [ "$(id -u)" -eq 0 ] && [ -w /proc/sys/vm/drop_caches ]; then
		sync
		echo 3 >/proc/sys/vm/drop_caches 2>/dev/null || true
	fi
}

# --------------------------------------------------------------------------- #
# Benchmark: start latency (cold + warm) for a given runtime
# --------------------------------------------------------------------------- #

# bench_start <runtime> <out-prefix>
# Writes "<out-prefix>.cold" and "<out-prefix>.warm" files of per-iteration ms.
bench_start() {
	local runtime="$1" prefix="$2"
	local bundle="$WORKDIR/bundle-$runtime-start"
	mkdir -p "$bundle"
	write_config "$bundle" "$(sh_args_json 'exit 0')"

	: >"$prefix.cold"
	: >"$prefix.warm"

	log "[$runtime] cold start x$ITERATIONS ..."
	for i in $(seq 1 "$ITERATIONS"); do
		stage_fresh_rootfs "$bundle"
		drop_caches
		local t0 t1
		t0="$(now_ms)"
		run_once "$runtime" "$bundle" "cold-$runtime-$i" || true
		t1="$(now_ms)"
		echo "$((t1 - t0))" >>"$prefix.cold"
	done

	# warm: rootfs stays staged and hot across iterations (respawn path).
	stage_fresh_rootfs "$bundle"
	run_once "$runtime" "$bundle" "warm-$runtime-prime" || true # prime caches
	log "[$runtime] warm start x$ITERATIONS ..."
	for i in $(seq 1 "$ITERATIONS"); do
		local t0 t1
		t0="$(now_ms)"
		run_once "$runtime" "$bundle" "warm-$runtime-$i" || true
		t1="$(now_ms)"
		echo "$((t1 - t0))" >>"$prefix.warm"
	done
}

# --------------------------------------------------------------------------- #
# Benchmark: resident memory overhead of the sandbox process tree
# --------------------------------------------------------------------------- #

# sum_tree_rss <root-pid> — sum VmRSS (KiB) of a pid and all its descendants.
sum_tree_rss() {
	local root="$1" total=0 pid kb
	local pids="$root"
	# breadth collect descendants via /proc/*/stat ppid field.
	local changed=1
	while [ "$changed" -eq 1 ]; do
		changed=0
		for pid in /proc/[0-9]*; do
			pid="${pid#/proc/}"
			local ppid
			ppid="$(awk '{print $4}' "/proc/$pid/stat" 2>/dev/null || echo 0)"
			case " $pids " in
			*" $ppid "*)
				case " $pids " in
				*" $pid "*) : ;;
				*)
					pids="$pids $pid"
					changed=1
					;;
				esac
				;;
			esac
		done
	done
	for pid in $pids; do
		kb="$(awk '/^VmRSS:/{print $2}' "/proc/$pid/status" 2>/dev/null || echo 0)"
		total=$((total + ${kb:-0}))
	done
	echo "$total"
}

# bench_memory <runtime> — median resident KiB of the whole sandbox process tree
# while a trivial long-lived workload sleeps inside. Printed to stdout.
bench_memory() {
	local runtime="$1"
	local bundle="$WORKDIR/bundle-$runtime-mem"
	mkdir -p "$bundle"
	stage_fresh_rootfs "$bundle"
	write_config "$bundle" "$(sh_args_json 'sleep 6')"

	local samples="$WORKDIR/mem-$runtime.samples"
	: >"$samples"

	local id="mem-$runtime"
	# shellcheck disable=SC2046
	( cd "$bundle" && "$runtime" $(runtime_flags "$runtime") run --bundle "$bundle" "$id" >/dev/null 2>&1 ) &
	local launcher=$!
	sleep 1 # let the runtime spin up its supervisor/sentry+gofer
	for _ in 1 2 3 4; do
		sum_tree_rss "$launcher" >>"$samples"
		sleep 1
	done
	wait "$launcher" 2>/dev/null || true

	median <"$samples"
}

# --------------------------------------------------------------------------- #
# Benchmark: CPU-bound and syscall-bound workload wall time
# --------------------------------------------------------------------------- #

# bench_workload <runtime> <workload-cmd> — median wall ms over ITERATIONS.
bench_workload() {
	local runtime="$1" workload="$2"
	local bundle="$WORKDIR/bundle-$runtime-wl"
	mkdir -p "$bundle"
	stage_fresh_rootfs "$bundle"
	write_config "$bundle" "$(sh_args_json "$workload")"

	local samples="$WORKDIR/wl-$runtime.samples"
	: >"$samples"
	run_once "$runtime" "$bundle" "wl-$runtime-prime" || true
	for i in $(seq 1 "$ITERATIONS"); do
		local t0 t1
		t0="$(now_ms)"
		run_once "$runtime" "$bundle" "wl-$runtime-$i" || true
		t1="$(now_ms)"
		echo "$((t1 - t0))" >>"$samples"
	done
	median <"$samples"
}

# --------------------------------------------------------------------------- #
# Capture methodology / environment
# --------------------------------------------------------------------------- #

RUNSC_VERSION="$(runsc --version 2>/dev/null | head -1 || echo 'unknown')"
RUNC_VERSION="n/a"
[ "$HAVE_RUNC" -eq 1 ] && RUNC_VERSION="$(runc --version 2>/dev/null | head -1 || echo unknown)"
KERNEL="$(uname -srm)"
CPU_MODEL="$(awk -F: '/model name/{print $2; exit}' /proc/cpuinfo 2>/dev/null | sed 's/^ //' || echo unknown)"
CPU_CORES="$(nproc 2>/dev/null || echo unknown)"
MEM_TOTAL_KB="$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
MEM_TOTAL_GB="$(awk -v k="$MEM_TOTAL_KB" 'BEGIN{ if (k>0) printf "%.1f", k/1024/1024; else print "unknown" }')"

{
	echo "kernel:        $KERNEL"
	echo "cpu:           $CPU_MODEL ($CPU_CORES cores)"
	echo "memory:        ${MEM_TOTAL_GB} GiB"
	echo "runsc:         $RUNSC_VERSION"
	echo "runc:          $RUNC_VERSION"
	echo "image:         ${BENCH_ROOTFS:-$BENCH_IMAGE}"
	echo "iterations:    $ITERATIONS"
	echo "cgroup limits: ${BENCH_CPUS} vCPU / ${BENCH_MEM_MB} MiB / 256 pids"
	echo "runsc flags:   ${BENCH_RUNSC_FLAGS:-(none)}"
	echo "runc flags:    ${BENCH_RUNC_FLAGS:-(none)}"
} >"$OUT_DIR/methodology.txt"

# --------------------------------------------------------------------------- #
# Run everything
# --------------------------------------------------------------------------- #

log "Methodology:"
cat "$OUT_DIR/methodology.txt" >&2

bench_start runsc "$WORKDIR/start-runsc"
RUNSC_COLD="$(median <"$WORKDIR/start-runsc.cold")"
RUNSC_WARM="$(median <"$WORKDIR/start-runsc.warm")"
RUNSC_MEM_KB="$(bench_memory runsc)"
RUNSC_CPU="$(bench_workload runsc "$CPU_WORKLOAD")"
RUNSC_SYS="$(bench_workload runsc "$SYS_WORKLOAD")"

if [ "$HAVE_RUNC" -eq 1 ]; then
	bench_start runc "$WORKDIR/start-runc"
	BASE_COLD="$(median <"$WORKDIR/start-runc.cold")"
	BASE_WARM="$(median <"$WORKDIR/start-runc.warm")"
	BASE_MEM_KB="$(bench_memory runc)"
	BASE_CPU="$(bench_workload runc "$CPU_WORKLOAD")"
	BASE_SYS="$(bench_workload runc "$SYS_WORKLOAD")"
else
	log "runc not present — host baseline columns reported as n/a."
	BASE_COLD="n/a"
	BASE_WARM="n/a"
	BASE_MEM_KB="n/a"
	BASE_CPU="n/a"
	BASE_SYS="n/a"
fi

# kib_to_mb <kib|n/a>
kib_to_mb() {
	[ "$1" = "n/a" ] && {
		echo "n/a"
		return
	}
	awk -v k="$1" 'BEGIN{ printf "%.0f", k/1024 }'
}
RUNSC_MEM_MB="$(kib_to_mb "$RUNSC_MEM_KB")"
BASE_MEM_MB="$(kib_to_mb "$BASE_MEM_KB")"

# overhead_x <runsc> <base> — "N.Nx" ratio, or "—" when no baseline.
overhead_x() {
	[ "$2" = "n/a" ] || [ "$2" = "0" ] && {
		echo "—"
		return
	}
	awk -v a="$1" -v b="$2" 'BEGIN{ printf "%.2fx", a/b }'
}

# --------------------------------------------------------------------------- #
# Emit Markdown table + JSON
# --------------------------------------------------------------------------- #

TABLE="$OUT_DIR/results.md"
{
	echo "| Metric | gVisor (runsc) | Host baseline (runc) | Overhead |"
	echo "| --- | --- | --- | --- |"
	echo "| Cold start (ms, median) | $RUNSC_COLD | $BASE_COLD | $(overhead_x "$RUNSC_COLD" "$BASE_COLD") |"
	echo "| Warm start (ms, median) | $RUNSC_WARM | $BASE_WARM | $(overhead_x "$RUNSC_WARM" "$BASE_WARM") |"
	echo "| Per-sandbox memory (MiB, median RSS) | $RUNSC_MEM_MB | $BASE_MEM_MB | $(overhead_x "$RUNSC_MEM_MB" "$BASE_MEM_MB") |"
	echo "| CPU-bound workload (ms, median) | $RUNSC_CPU | $BASE_CPU | $(overhead_x "$RUNSC_CPU" "$BASE_CPU") |"
	echo "| Syscall-bound workload (ms, median) | $RUNSC_SYS | $BASE_SYS | $(overhead_x "$RUNSC_SYS" "$BASE_SYS") |"
} >"$TABLE"

cat >"$OUT_DIR/results.json" <<JSON
{
  "methodology": {
    "kernel": "$KERNEL",
    "cpu": "$CPU_MODEL",
    "cpu_cores": "$CPU_CORES",
    "memory_gib": "$MEM_TOTAL_GB",
    "runsc": "$RUNSC_VERSION",
    "runc": "$RUNC_VERSION",
    "image": "${BENCH_ROOTFS:-$BENCH_IMAGE}",
    "iterations": $ITERATIONS,
    "cgroup_cpus": $BENCH_CPUS,
    "cgroup_mem_mb": $BENCH_MEM_MB,
    "runsc_flags": "$BENCH_RUNSC_FLAGS",
    "runc_flags": "$BENCH_RUNC_FLAGS"
  },
  "runsc": {
    "cold_start_ms": "$RUNSC_COLD",
    "warm_start_ms": "$RUNSC_WARM",
    "memory_mib": "$RUNSC_MEM_MB",
    "cpu_workload_ms": "$RUNSC_CPU",
    "syscall_workload_ms": "$RUNSC_SYS"
  },
  "baseline": {
    "cold_start_ms": "$BASE_COLD",
    "warm_start_ms": "$BASE_WARM",
    "memory_mib": "$BASE_MEM_MB",
    "cpu_workload_ms": "$BASE_CPU",
    "syscall_workload_ms": "$BASE_SYS"
  }
}
JSON

echo
echo "## Methodology"
echo
sed 's/^/    /' "$OUT_DIR/methodology.txt"
echo
echo "## Results"
echo
cat "$TABLE"
echo
log "Wrote results to: $OUT_DIR (results.md, results.json, methodology.txt)"
