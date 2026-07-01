# Sandbox performance & footprint

IronClaw runs every agent inside its own **gVisor (`runsc`) sandbox** —
`network=none`, all capabilities dropped, `no_new_privs`, non-root user
namespace, read-only rootfs, cgroup memory/CPU limits. That isolation is the
product. This page quantifies what it costs at runtime so you can size hosts and
judge the trade honestly.

!!! note "What this page is — and isn't"
    The overhead of gVisor is **workload-dependent**, so the only numbers worth
    trusting are the ones you measure on *your* hardware with *your* runtime
    version. This page ships a reproducible harness that does exactly that, the
    [numbers it produced on a public CI runner](#measured-on-ci) (every one of them
    inspectable and re-runnable), and a conservative expectation profile drawn from
    gVisor's own published performance guidance. We do **not** quote hero numbers
    from a machine you can't inspect.

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

## Measured on CI

The [`Sandbox benchmarks`](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/sandbox-bench.yml)
workflow runs this harness on a GitHub-hosted `ubuntu-24.04` runner, where `runsc`
actually launches (macOS cannot host gVisor's gofer, see IRO-116). It uploads
`results.md`, `results.json`, and `methodology.txt` as a build artifact and prints
the table to the run's job summary, so every number is inspectable and reproducible.

**Runner spec (representative run
[28514817897](https://github.com/IronSecCo/ironclaw/actions/runs/28514817897)):**

| | |
| --- | --- |
| Runner | GitHub-hosted `ubuntu-24.04` |
| Kernel | Linux 6.17.0-1018-azure x86_64 |
| CPU | Intel Xeon Platinum 8370C (4 vCPU on the runner) |
| Memory | 15.6 GiB |
| Isolation runtime | `runsc` release-20260622.0, platform `systrap`, `--network=none` |
| Baseline runtime | `runc` 1.3.6 |
| Sampling | median of 25 iterations, cgroup limit 1 vCPU / 512 MiB |

**Measured numbers:**

| Metric | gVisor (runsc) | Host baseline (runc) | Added by gVisor |
| --- | --- | --- | --- |
| Cold start (ms, median) | 77 | 38 | +39 ms (2.0x) |
| Warm start (ms, median) | 18 | 5 | +13 ms (3.6x) |
| CPU-bound short task (ms, median) | 21 | 5 | +16 ms |
| Syscall-bound short task (ms, median) | 21 | 5 | +16 ms |
| Per-sandbox memory (MiB) | not captured in CI (see note) | not captured in CI | see note |

**How to read this run:**

- **The gVisor cost is a fixed, one-time launch overhead:** about **+13 ms** on a
  warm respawn and **+39 ms** on a cold new-agent launch. It is paid once per sandbox
  launch, not per request. (The GitHub-hosted runner is a shared VM whose CPU model
  varies between runs, so absolute figures move a few milliseconds each time; the
  additive launch cost is the stable signal.)
- **The CPU and syscall micro-workloads land within 3 ms of the warm-start time**
  (21 and 21 ms versus 18 ms). At this workload size they are dominated by the launch
  itself, so they confirm that light in-sandbox work adds very little on top of the
  launch, but they do **not** isolate steady-state syscall overhead. The apparent
  ~4x ratio there is a ratio of two small launch-dominated numbers, not a 4x tax on
  compute. To measure the steady-state syscall gap, run a heavier, longer workload on
  a real host with the same harness.
- **Absolute values are small and the runner is shared,** so expect run-to-run
  variation. The durable signal is the additive launch cost and the reproducible
  method, not any single millisecond figure.

!!! note "Why per-sandbox memory reads as not-captured here"
    gVisor's Sentry and Gofer run as **detached host processes**. On this locked-down
    hosted runner they are not attributable through the container cgroup, the launcher
    process tree, or process names, so the sampler reports 0. That is an environment
    limitation, **not** a measurement of zero overhead. On a real host the same harness
    attributes the supervisor RSS (expect **tens of MiB** per sandbox, consistent with
    gVisor's guidance below), and it emits a `mem-snapshot-<runtime>.txt` artifact to
    help you attribute it on your own hardware.

## What to expect

The expectation profile below is drawn from gVisor's published guidance. The CI run
above is consistent with it: near-zero marginal cost for light in-sandbox work, a
bounded additive launch cost, and a memory footprint that only a real host, not a
locked-down CI runner, can attribute.

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
