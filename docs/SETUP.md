# SETUP — Local build & test

Foundation notes for standing up an IronClaw working copy and proving it builds
and tests green. See also `docs/building.md` and `docs/quickstart.md`.

## Prerequisites

- **Go 1.23+** (developed/verified on go1.26.2).
- **`CGO_ENABLED=1`** — the encrypted-SQLite queues use cgo
  (`github.com/mutecomm/go-sqlcipher/v4`). A pure-Go build will not link.
- **A C toolchain** — Apple `clang` on macOS, `gcc`/`clang` on Linux.
- **git**.

## Build

```sh
export CGO_ENABLED=1
make build          # == go build ./...
```

Verified green on macOS (darwin/amd64, go1.26.2, Apple clang 21) — ~9s cold.

## Test

```sh
export CGO_ENABLED=1
make test           # == go test ./...
```

`make test` mirrors CI (`.github/workflows/ci.yml`, `CGO_ENABLED=1 go test ./...`).
On CI (Linux, ≤4 cores) this runs green as-is.

Current state: **32 packages pass, 0 failures, 6 packages have no tests**
(~701 top-level `Test` funcs, ~800 incl. subtests). Per-package runtime <1.5s.

### macOS caveat — cap test parallelism

`go test ./...` runs one test binary per package concurrently, up to
`GOMAXPROCS` (= **12** on this 12-logical-core box). Twelve cgo/SQLCipher test
binaries plus the agent-harness sandbox oversubscribe the CPU; a parallel batch
makes such slow progress that every binary in it trips its per-binary timeout at
once. Symptom: ~12 packages all `FAIL ... 150.001s` (or a single package killed
at the 10-minute default), while the rest pass.

This is **resource oversubscription, not a code break** — every affected package
passes in isolation, serially, and at reduced parallelism. On macOS, cap it:

```sh
CGO_ENABLED=1 go test -p 4 ./...   # all 32 pkgs green, ~15s total
CGO_ENABLED=1 go test -p 1 ./...   # serial, fully green, ~16s total
```

`-p 4` is the recommended local default on this machine. (CI is unaffected — its
Linux runners have ≤4 cores, so `make test` already runs at low effective
parallelism there.)

## Platform caveats — isolation (gVisor)

Full sandbox isolation uses gVisor (`runsc`) / Kata and is **Linux-only**.

- macOS has no `runsc`. Two environment-gated tests cover the real path and
  `t.Skip` cleanly where gVisor is absent — neither weakens a trust boundary to
  pass:
  - `TestRunscRealLaunch` (`internal/host/isolation`) does an **actual `runsc
    run`** of a hardened bundle: it stages a tiny static probe as `/sandbox` in a
    from-scratch rootfs, launches it through the production `RunscIsolator.Launch`
    path, and asserts from inside the live sandbox that the inbound queue is
    read-only, the outbound queue is writable, and `network=none` holds (no
    non-loopback interfaces). Opt in on a gVisor host with
    `IRONCLAW_RUNSC_INTEGRATION=1 go test -run TestRunscRealLaunch ./internal/host/isolation`
    (point at an alternate runtime with `IRONCLAW_RUNSC_BIN`).
  - `TestFullLifecycleRunscGated` (`test/e2e/lifecycle_test.go`) detects `runsc`
    via `exec.LookPath` and checks the real isolator constructs and the hardened
    spec builds for the runsc path.

  On the normal install-and-run flow, pass `--sandbox-provisioner=containerd`
  with a pinned `--sandbox-image-digest sha256:<hex>` so the control plane pulls
  and unpacks `--sandbox-image` host-side (via `ctr`) and refuses any image whose
  resolved digest does not match the pin. `containerd` without a digest pin is a
  fatal misconfiguration (fail closed). `--sandbox-provisioner=none` (the
  default) requires the operator to pre-stage each session's rootfs under
  `--bundle-root/<session>/rootfs`; `Launch` fails closed (`ErrRootfsMissing`)
  rather than launch an empty rootfs.
- The isolation unit tests (`internal/host/isolation`, `internal/host/mcp`)
  verify OCI-spec construction and the hardened spec (`network=none`, RO mounts,
  seccomp, model-proxy socket) without needing a live `runsc`, so they run and
  pass on macOS.
- On macOS the runtime path falls back to Docker `runc` (no gVisor kernel
  isolation). Use Linux + gVisor for the real security posture; macOS is for
  development only.

There are no `//go:build linux` constraints in the Go sources/tests — the code is
cross-platform at the Go level and detects `runsc` availability at runtime.
