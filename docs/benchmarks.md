# Sandbox performance & footprint

IronClaw runs every agent inside its own **gVisor (`runsc`) sandbox** —
`network=none`, all capabilities dropped, `no_new_privs`, non-root user
namespace, read-only rootfs, cgroup memory/CPU limits. That isolation is the
product. This page quantifies what it costs at runtime so you can size hosts and
judge the trade honestly.

!!! note "What this page is — and isn't"
    The overhead of gVisor is **workload-dependent**, so the only numbers worth
    trusting are the ones you measure on *your* hardware with *your* runtime
    version. This page ships a reproducible harness that does exactly that, plus
    a conservative expectation profile drawn from gVisor's own published
    performance guidance. We do **not** quote hero numbers from a machine you
    can't inspect.

## Reproduce it yourself

A committed harness, [`scripts/bench/sandbox-bench.sh`](https://github.com/IronSecCo/ironclaw/blob/main/scripts/bench/sandbox-bench.sh),
launches a minimal OCI bundle whose `config.json` mirrors the real IronClaw trust
boundary (the same fields [`internal/host/isolation`](https://github.com/IronSecCo/ironclaw/blob/main/internal/host/isolation/isolation.go)
emits) and times it under `runsc` versus a host baseline (`runc`). Because the
*same bundle* runs under both runtimes, everything that is not the isolation
layer cancels out — the delta is the gVisor wall, nothing else.

```bash
# On a Linux host with gVisor installed (and, ideally, runc for the baseline):
scripts/bench/sandbox-bench.sh --iterations 50

# Pin the rootfs by digest for byte-for-byte repeatability:
BENCH_IMAGE=busybox@sha256:<digest> scripts/bench/sandbox-bench.sh
```

It prints a Markdown results table and writes `results.json` plus a
`methodology.txt` capturing kernel, CPU, RAM, and exact `runsc`/`runc` versions.

!!! warning "gVisor needs a real host"
    `runsc` requires a gofer-capable host kernel. It will **not** start inside a
    nested/locked-down CI runner (gofer creation fails with
    `fork/exec /proc/self/exe`). Run the harness on bare metal or a VM where
    `runsc do true` succeeds.

### What it measures

| Metric | Workload | Why it matters |
| --- | --- | --- |
| **Cold start** | launch from a freshly staged rootfs | time to spin up a *new* agent |
| **Warm start** | launch with the rootfs already cached | time to respawn an agent |
| **Per-sandbox memory** | resident RSS of the whole sandbox process tree (sentry + gofer) while a trivial workload idles | how many agents fit on a host |
| **CPU-bound** | a fixed integer loop, no syscalls in the hot path | compute overhead (gVisor's best case) |
| **Syscall-bound** | a stat-heavy filesystem walk | I/O overhead (gVisor's worst case) |

## What to expect

gVisor implements the Linux syscall surface in a user-space kernel (the
*Sentry*) and proxies filesystem I/O through a *Gofer*. The cost of that
indirection is highly uneven across workload classes. The profile below is
conservative and consistent with gVisor's published
[performance guide](https://gvisor.dev/docs/architecture_guide/performance/);
your harness run should land in the same ballpark.

| Dimension | Expected overhead vs a `runc` baseline |
| --- | --- |
| **CPU-bound compute** | **Near-native** — within a few percent. Work that stays in userspace barely touches the Sentry. |
| **Memory per sandbox** | A fixed per-sandbox cost (Sentry + Gofer), typically on the order of **tens of MiB** of RSS beyond the workload itself. |
| **Process / sandbox start** | A modest additive cost over `runc` — on the order of **a couple hundred milliseconds** for the Sentry and Gofer to come up. One-time per agent launch, not per request. |
| **Syscall- / I/O-heavy work** | The largest gap — stat/open/read-heavy paths can run **noticeably slower** (often in the ~1.5–2.5× range) because every syscall is mediated. This is the cost you are explicitly buying isolation with. |
| **Network throughput** | **Not applicable.** gVisor's network path is its weakest dimension — but IronClaw sandboxes run `network=none` with no NIC at all, so this overhead simply does not exist here. Egress, when granted, is a host-mediated unix socket, not in-sandbox networking. |

### Reading the trade-off

- **The overhead is bounded and predictable**, not a tax on every operation.
  CPU-bound agent reasoning is near-native; the cost concentrates on syscall- and
  I/O-heavy bursts.
- **The footprint is what makes density planning simple.** Per-sandbox memory is
  a roughly fixed additive cost, so host capacity scales linearly with agent
  count.
- **You pay once, at the boundary you actually care about.** gVisor's worst
  dimension (networking) is one IronClaw has already removed by design. What
  remains is the syscall-mediation cost — which *is* the isolation. See the
  [threat model](threat-model.md) for what that wall buys you and the
  [security posture](security.md) for the invariants it upholds.

## Methodology notes

- **Like-for-like.** The same OCI bundle is run under `runsc` and `runc`; the
  delta isolates the runtime, not image pulls, provisioning, or orchestration.
- **Conservative by construction.** Medians over N iterations (default 50), a
  warm-up priming run discarded, and a best-effort page-cache drop before each
  cold-start sample (when run as root).
- **Reproducible.** Rootfs is exported from a pinnable OCI image (pin by digest);
  every environment fact that affects the result is captured in
  `methodology.txt` alongside the numbers.
- **gVisor-only.** IronClaw positions on gVisor; these benchmarks make no claims
  about other runtimes (e.g. Kata) and the harness does not measure them.
