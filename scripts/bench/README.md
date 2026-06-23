# Sandbox benchmarks

`sandbox-bench.sh` measures the runtime overhead of IronClaw's gVisor (`runsc`)
sandbox versus a host baseline (`runc`), using an OCI bundle whose `config.json`
mirrors the real IronClaw trust boundary (`network=none`, no capabilities,
`no_new_privs`, non-root user namespace, read-only rootfs, cgroup mem/CPU limits;
see `internal/host/isolation`).

The published methodology and the expected-overhead profile live at
[`docs/benchmarks.md`](../../docs/benchmarks.md).

## Run it

```bash
# Linux host with gVisor installed; runc optional (used for the baseline columns).
scripts/bench/sandbox-bench.sh --iterations 50

# Reproducible rootfs (recommended): pin the source image by digest.
BENCH_IMAGE=busybox@sha256:<digest> scripts/bench/sandbox-bench.sh --out ./bench-out
```

Outputs `results.md`, `results.json`, and `methodology.txt` (kernel, CPU, RAM,
exact `runsc`/`runc` versions) into the results directory.

## Requirements

- `runsc` in `PATH` (the runtime under test). Must run on a gofer-capable host —
  it will not start inside a nested/locked-down CI runner.
- `runc` (optional) for the host-baseline columns.
- `docker` or `podman` to export a minimal rootfs once, **or** set
  `BENCH_ROOTFS=/path/to/rootfs` to supply a pre-staged one.

## Flags & env

| Flag / env | Default | Meaning |
| --- | --- | --- |
| `--iterations N` | `20` | samples per metric (medians are reported) |
| `--out DIR` | temp dir | where results are written |
| `--keep` | off | keep the scratch workdir for inspection |
| `BENCH_IMAGE` | `busybox:1.36.1` | image to source the rootfs from |
| `BENCH_ROOTFS` | — | use this extracted rootfs instead of pulling an image |
| `BENCH_MEM_MB` | `512` | cgroup memory limit (MiB) |
| `BENCH_CPUS` | `1` | cgroup CPU quota (vCPUs) |
