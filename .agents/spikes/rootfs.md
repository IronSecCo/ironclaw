# Spike T-012 — Rootfs / image-unpacker approach

> **Status:** recommendation (research only — no production code).
> **Gap:** G-008 — rootfs provisioning is the one remaining external integration
> point in `internal/host/isolation` (`isolation.go:179–215`, `ErrRootfsMissing`).
> **Feeds:** T-022 (rootfs provisioning implementation; owns
> `internal/host/isolation/**` + `deploy/**`).
> **Author:** claude-TopazsMBP-438b · **Base-SHA:** da75bf3

---

## 1. The integration point as it stands today

`RunscIsolator.Launch` (`internal/host/isolation/isolation.go:179`) already does
everything except populate the root filesystem:

1. `WriteBundle` builds the hardened OCI spec and writes
   `<BundleRoot>/<sessionID>/config.json` (`isolation.go:149`). The spec sets
   `Root.Path = "rootfs"` (bundle-relative) and `Root.Readonly = true`
   (`oci.go:167`).
2. Launch then `stat`s `<bundle>/rootfs`; if it is not a directory it returns
   `ErrRootfsMissing` **without** execing the runtime (`isolation.go:187`).
3. Otherwise it execs `<runtime> run --bundle <dir> <id>` (`isolation.go:199`).

So "provisioning rootfs" means exactly: **make `<bundle>/rootfs/` a populated
directory tree containing the sandbox image's filesystem, before the stat check.**
The OCI process is fixed — `Args: ["/sandbox"]`, `Cwd: /workspace`
(`oci.go:160-162`) — so the rootfs only ever needs to contain the **single
`/sandbox` image** plus its runtime deps. `/workspace` is a writable tmpfs mount,
the queues/socket are bind mounts, and the rootfs itself is mounted **read-only**.

Three facts drive the whole decision:

- **One image, not many.** Every sandbox runs the same control-plane-built
  `/sandbox` image. `SandboxSpec.Image` exists but in practice resolves to one
  pinned reference. We do not need a general multi-image registry client.
- **Read-only rootfs.** Because the rootfs is RO and `/workspace`/queues are
  separate mounts, the *same* extracted filesystem is safe to share across every
  concurrent session. Per-session copies are unnecessary.
- **Two hard constraints from the threat model & repo ethos:**
  - The control-plane tree is deliberately **stdlib-only** (`oci.go:9-15` refuses
    even to import `opencontainers/runtime-spec`). A heavy vendored client library
    cuts against that grain.
  - Image pull happens **host-side**. The sandbox is `network=none` with no
    package install (AGENTS.md §10); nothing is fetched from inside the sandbox.

---

## 2. Options considered

### Option A — containerd image service (already a declared host dependency)

`deploy/README.md` already mandates **containerd + gVisor**, running sandboxes
under the `io.containerd.runsc.v1` runtime. containerd therefore is *not a new
dependency* — it is assumed present in production. Its content store + snapshotter
pull and unpack OCI images natively.

Two integration styles:

- **A1 — Go client** (`github.com/containerd/containerd`): rich, but pulls a large
  transitive dependency graph into a tree that intentionally has none. Rejected on
  ethos grounds (see §1).
- **A2 — shell out to `ctr`/containerd CLI over `os/exec`**: mirrors exactly how
  `RunscIsolator` already invokes `runsc` (`isolation.go:199`) and how
  `deploy/install.sh` provisions host tools. Zero new Go deps. **Preferred style.**

### Option B — standalone OCI unpacker (`skopeo`+`umoci`, or `go-containerregistry`/`crane`)

Pull an image and flatten its layers into a directory with no container runtime
involved. `crane export` / `umoci unpack` produce a rootfs tree directly.

- As Go libraries: same heavy-dependency objection as A1.
- As external binaries shelled out: viable and *runtime-agnostic* (works even if a
  future backend isn't containerd-based), but it adds **another** host binary to
  install and maintain alongside containerd, which already does this job.

### Option C — pre-baked rootfs tarball (build once, extract at deploy)

Build the `/sandbox` image at release time, ship a flat tarball, extract it once on
the host into a shared rootfs directory. No runtime image pull at all. Simplest
possible; but loses content-addressed integrity/verification and couples deploy
tooling to an out-of-band artifact. Good *fallback*, weak *primary*.

---

## 3. Recommendation

**Use containerd (Option A2) as the primary provisioner, shelled out via
`os/exec`, behind a small pluggable `RootfsProvisioner` interface — and exploit the
single-RO-image fact by extracting once into a shared, content-addressed rootfs
that every session binds read-only.**

Rationale, tied to the codebase:

1. **No new dependency.** containerd is already required by `deploy/README.md`;
   reusing it avoids introducing a second image toolchain (the objection to B) and
   keeps the control-plane tree stdlib-only (the objection to A1/B-as-library).
2. **Consistent with existing patterns.** Shelling out matches `runsc`
   invocation and the "runtime binary is overridable / pluggable Isolator" design
   already in `isolation.go`. Provisioning becomes one more pluggable seam.
3. **Right altitude for one RO image.** A shared extracted rootfs keyed by image
   digest is pulled/unpacked once and reused; per-session bundles get a read-only
   bind of it. This is essentially what a snapshotter does, without taking a
   library dependency on one.
4. **Keeps the safety post-condition.** The existing `stat`/`ErrRootfsMissing`
   gate stays as a *post-condition* after provisioning, so a broken provisioner
   still fails loudly instead of launching an empty rootfs
   (`TestLaunchRequiresProvisionedRootfs` keeps passing unchanged).

Keep B (standalone unpacker) as a documented drop-in: because the seam is an
interface, a `umoci`/`crane`-backed provisioner can replace the containerd one with
no change to `Launch`. Keep C as the air-gapped fallback (a `tarProvisioner`).

---

## 4. Integration sketch for `isolation.Launch` (for T-022)

### 4.1 A pluggable provisioner seam

```go
// RootfsProvisioner populates a bundle's rootfs/ directory with the sandbox
// image's filesystem. Implementations must be IDEMPOTENT (a no-op when the
// rootfs is already present) and must NOT require network access from inside any
// sandbox — image pull is a host-side action. nil provisioner == today's
// behavior (caller must pre-stage rootfs, else ErrRootfsMissing).
type RootfsProvisioner interface {
    Provision(ctx context.Context, image string, rootfsDir string) error
}
```

Add an optional field + option to `RunscIsolator`, defaulting to `nil` so existing
tests and rootfs-pre-staged hosts are unchanged:

```go
type RunscIsolator struct {
    RuntimeBinary string
    BundleRoot    string
    Provisioner   RootfsProvisioner // optional; nil preserves current behavior
}

func WithProvisioner(p RootfsProvisioner) Option {
    return func(r *RunscIsolator) { if p != nil { r.Provisioner = p } }
}
```

### 4.2 The Launch hook (one inserted block, gate preserved)

```go
func (r *RunscIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
    bundleDir, err := r.WriteBundle(spec)
    if err != nil {
        return nil, err
    }
    rootfsDir := filepath.Join(bundleDir, "rootfs")

    // NEW: provision out of band if a provisioner is configured.
    if r.Provisioner != nil {
        if err := r.Provisioner.Provision(ctx, spec.Image, rootfsDir); err != nil {
            return nil, fmt.Errorf("host/isolation: provision rootfs for %q: %w", spec.Image, err)
        }
    }

    // UNCHANGED post-condition: still fail loudly if rootfs is absent.
    if fi, statErr := os.Stat(rootfsDir); statErr != nil || !fi.IsDir() {
        return nil, fmt.Errorf("host/isolation: rootfs not provisioned at %s ...: %w", rootfsDir, ErrRootfsMissing)
    }
    // ... unchanged exec of <runtime> run --bundle ...
}
```

This keeps `config.json` written before the rootfs check (the invariant
`TestLaunchRequiresProvisionedRootfs` asserts) and leaves `WriteBundle` untouched.

### 4.3 The containerd-backed provisioner (shared, content-addressed)

```go
// containerdProvisioner pulls + unpacks the image once into a shared,
// digest-keyed rootfs, then read-only binds it into each bundle's rootfs/.
type containerdProvisioner struct {
    ctrBinary  string // default "ctr"
    namespace  string // default "ironclaw"
    sharedRoot string // e.g. <BundleRoot>/_rootfs
}
```

Provision steps (each an `os/exec` call, mirroring the runsc exec style):

1. **Resolve digest** for `image` (`ctr -n ironclaw images pull <image>` is
   idempotent; pull is skipped if content already present). Pull runs on the host,
   which has network — never inside a sandbox.
2. **Materialize once** into `sharedRoot/<digest>/`: either
   `ctr -n ironclaw images mount <image> <dir>` (snapshotter view) or export +
   extract the image rootfs. Guarded so concurrent sessions extract once (lockfile
   / `O_EXCL` marker under `sharedRoot/<digest>/.ready`).
3. **Bind read-only** `sharedRoot/<digest>` → `<bundle>/rootfs` (bind mount, or for
   environments without mount privileges, fall back to a reflink/copy). The bundle
   keeps `Root.Path = "rootfs"`, so `config.json` needs no change.

`Stop` (`isolation.go:220`) gains a symmetric unmount of the per-session bind
before/after `runsc delete`; the shared `sharedRoot/<digest>` is left intact for
reuse and garbage-collected separately.

### 4.4 Deploy + daemon wiring (the `deploy/**` half of T-022)

- `deploy/install.sh`: document/automate `ctr` availability (it ships with
  containerd, already installed per `deploy/README.md` §Components.1) and the
  one-time pull of the pinned `/sandbox` image.
- `cmd/controlplane` (composed later under **T-016**, not T-022): construct the
  isolator with `isolation.NewRunsc(WithProvisioner(containerdProvisioner{...}))`.
  T-022 should expose the provisioner constructor but **must not** edit
  `cmd/controlplane/main.go` (that file is T-016's sole-owner scope).

---

## 5. Test strategy for T-022 (no real containerd in CI)

The interface makes this clean and keeps CI hermetic (CI has no `runsc`/`ctr`):

- **Fake provisioner** in tests: `Provision` just `os.MkdirAll(rootfsDir, 0o700)`
  and drops a marker file. Launch then passes the rootfs gate and proceeds to the
  runtime exec, which is already exercised with a fake/absent runtime binary in the
  existing tests. This directly extends `TestLaunchRequiresProvisionedRootfs`
  (`isolation_test.go:180`) with its mirror: *provisioned rootfs ⇒ no
  `ErrRootfsMissing`*.
- **Idempotency test:** calling the fake provisioner twice is a no-op (second call
  sees `.ready`).
- **Post-condition test:** a provisioner that returns `nil` but leaves `rootfs/`
  empty must still yield `ErrRootfsMissing` (proves the gate is a real
  post-condition, not bypassed).
- **containerd path:** integration-tagged (`//go:build integration`) and skipped by
  default so `CGO_ENABLED=1 go test ./...` stays green on hosts without containerd.

---

## 6. Risks, non-goals, open questions

- **Bind-mount privileges.** Creating the per-session RO bind may need
  `CAP_SYS_ADMIN` on the host (the *host*, not the sandbox). If the deploy target
  forbids it, fall back to copy/reflink into `<bundle>/rootfs`. T-022 should make
  the materialization step pluggable (bind | reflink | copy).
- **Shared-rootfs GC.** A digest-keyed shared rootfs needs a reaper (drop unused
  digests). Out of scope for the first T-022 cut; note it.
- **Non-goals (do NOT build):** in-sandbox image pull / package install
  (AGENTS.md §10), a multi-image registry client, or a second image toolchain
  alongside containerd. Image references stay host-side and pinned.
- **Open question for T-022:** snapshotter `ctr images mount` vs export-and-extract
  for step 4.3.2 — both work; pick based on whether the chosen snapshotter
  (overlayfs) is available on the deploy host. Default to overlayfs mount, fall
  back to extract.
- **Contract is untouched.** None of this approaches `internal/contract/**`; the
  rootfs is purely a runtime/isolation concern, so no RFC / `lock:contract` is
  needed.

---

## 7. One-line recommendation

Provision rootfs with **containerd, shelled out via `os/exec`**, behind a
**`RootfsProvisioner` interface** that **unpacks the single read-only sandbox image
once into a digest-keyed shared rootfs** and **binds it read-only per session** —
keeping the tree stdlib-only, reusing an already-required host dependency, and
leaving the existing `ErrRootfsMissing` gate intact as a safety post-condition.
