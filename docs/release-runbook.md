# IronClaw Release Runbook

Operational guide for cutting, verifying, and yanking an IronClaw release.

**Owner:** Relay (Release Engineer). **Scope:** `.github/workflows/release.yml`,
`.github/workflows/image.yml`, `scripts/install.sh`, `scripts/install.ps1`.

This runbook documents the pipeline as it ships on `main`. Where a section names an
in-flight ticket (e.g. the post-release smoke gate), that capability is landing
separately and the runbook is updated when it merges.

> **Trust model (non-negotiable).** A release a user cannot verify is not a secured
> release. Every published set is checksummed, the checksum file is signed keylessly
> with cosign, and every archive carries a build-provenance attestation tied to the
> source commit and workflow. Never weaken or skip signing, checksums, or attestation
> to make a build go green — yank a bad release instead (see [Yanking a release](#6-yanking-a-release)).

---

## 1. Pipeline at a glance

A release is cut automatically on every push to `main` (and can be run manually). Three
GitHub Actions workflows are involved:

| Workflow | File | Trigger | Produces |
|----------|------|---------|----------|
| **CI** | `ci.yml` | every push + PR | `build` / `vet` / `test` with `CGO_ENABLED=1` (gating check) |
| **Release** | `release.yml` | push to `main`, or `workflow_dispatch` | tag, GitHub Release, archives, `SHA256SUMS`, SBOMs, cosign signature, attestations |
| **Image** | `image.yml` | `workflow_run` after Release succeeds, or `workflow_dispatch` | GHCR control-plane image (`ghcr.io/<owner>/ironclaw-controlplane`) + image attestation |

The **Release** workflow runs as a chain of jobs, each gated on the previous:

```
version ──> build (5-target matrix) ──> release ──> [smoke]
   │             │                          │           │
 derive tag   CGO build per OS/arch    checksums,    install via install.sh
 (skip if     archive + upload         GH Release,   on each target, assert
  already                              SBOM, cosign  "Checksum OK" + version
  tagged)                              sign, attest  (fail-closed gate)
```

- `build` uses `fail-fast: false` but `release` `needs: [version, build]`, so **a release
  is published only if every matrix target built** — no partial release sets.
- All post-publish steps (SBOM, cosign signature, attestation) run **after** the binaries
  and checksums are uploaded, so a Sigstore/tooling hiccup cannot block the artifacts from
  shipping. See [Partial-failure semantics](#5-partial-failure-semantics-operator-decision) — this is the one
  case that needs an operator decision.

---

## 2. How the version tag is derived

The version is **`v<BASE_VERSION>.<total commit count>`**, e.g. `v0.1.123`.

- `BASE_VERSION` is hard-coded in the `version` job of `release.yml` (currently `0.1`).
  **Bump it there** to roll the major/minor (e.g. set `0.2` to start the `v0.2.x` line).
- The patch number is `git rev-list --count HEAD` — the total number of commits reachable
  from the released commit. It is monotonic, needs no manual bumps, and ties the tag to one
  exact commit.
- The resolved tag is stamped into the binaries at build time via
  `-ldflags "-X github.com/IronSecCo/ironclaw/internal/version.Version=<tag>"`, so
  `ironctl version` reports the exact release tag. (Unstamped/source builds report `dev`.)

**Idempotency / re-run safety.** The `version` job checks whether the tag already exists
(`git rev-parse --verify refs/tags/<tag>`). If it does, `exists=true` and the `build`,
`release`, and `smoke` jobs are all skipped (`if: needs.version.outputs.exists == 'false'`).
This means **re-running Release on a commit that is already released is a safe no-op** — it
will not overwrite or duplicate an existing release.

**Consequence for yanking.** Because the patch number is the commit count, the only way to
get the *same* tag again is to release the *same* commit. If you delete a tag and push a new
commit, the count increments and you get a *new* tag — you cannot accidentally collide with
a yanked tag's number. If you delete a tag and re-run Release on the *same* commit, it
rebuilds cleanly (`exists` is `false` again).

---

## 3. How to cut a release

### 3.1 The normal path (automatic)

Merge to `main`. The Release workflow fires on `push: branches: [main]`, derives the next
`v0.1.<count>` tag, builds the matrix, and publishes the Release + tag at the merged commit.
No manual action is required. Releases are serialized (`concurrency: group: release`,
`cancel-in-progress: false`) so two pushes never race or orphan a tag.

Watch the run:

```sh
gh run list --workflow=release.yml --limit 5
gh run watch <run-id>
```

A green Release run means: all five archives built, `SHA256SUMS` written and signed, SBOMs
and the cosign signature/cert attached, and provenance attested. The Image workflow then
chains off the success and publishes the GHCR image for that commit.

### 3.2 Manual dispatch (re-cut or pin a specific tag)

Use `workflow_dispatch` when you need to re-run a release or stamp a specific tag:

```sh
# Auto-derive the tag (same as a push to main):
gh workflow run release.yml

# Override the tag explicitly (e.g. to re-cut after a yank, or hotfix a specific number):
gh workflow run release.yml -f version=v0.1.99
```

If the supplied/derived tag already exists, the run is a safe no-op (see §2).

### 3.3 The build matrix (must stay in sync with the README)

| OS / arch | Runner | C toolchain |
|-----------|--------|-------------|
| `darwin/amd64` | `macos-14` | clang, cross via `CGO_CFLAGS/LDFLAGS=-arch x86_64` on the universal SDK |
| `darwin/arm64` | `macos-14` | native clang |
| `linux/amd64` | `ubuntu-latest` | native gcc |
| `linux/arm64` | `ubuntu-latest` | cross `aarch64-linux-gnu-gcc` (`apt: gcc-aarch64-linux-gnu`) |
| `windows/amd64` | `windows-latest` | native mingw-w64 gcc |

Every target builds with **`CGO_ENABLED=1`, Go 1.23** — CGO is mandatory because the
encrypted-SQLite (SQLCipher) binding compiles a vendored C amalgamation. A pure-Go assumption
will break the build. If you change the matrix, confirm each target still compiles with cgo,
and update the README's Platform support / Installation tables to match — a platform that
silently drops out of the matrix is a release defect.

### 3.4 What a successful release contains

Attached to the GitHub Release for tag `<tag>` (version `<ver>` = tag without the leading `v`):

- `ironclaw_<ver>_<os>_<arch>.tar.gz` (and `.zip` for Windows) — one per matrix target.
  Each archive holds `ironctl`, `ironclaw-controlplane`, `ironclaw-sandbox`, `LICENSE`, `README.md`.
- `SHA256SUMS` — checksums of every archive (**the trust anchor**).
- `SHA256SUMS.sig` + `SHA256SUMS.pem` — the keyless cosign signature and its certificate.
- `ironclaw_<ver>.spdx.json` + `ironclaw_<ver>.cdx.json` — SBOMs (syft, SPDX + CycloneDX).
- Build-provenance attestations for each archive **and for each raw binary**
  (`ironctl`, `ironclaw-controlplane`, `ironclaw-sandbox` on every platform), so
  `gh attestation verify` works whether you point it at the downloaded `.tar.gz`/`.zip`
  or at a binary extracted from it.

### 3.5 Post-release verification gate (smoke — in flight, IRO-15)

A `smoke` job installs the freshly-cut release through the real, checksum-verifying
`scripts/install.sh` (the normal user path, **not** `--dev`) on `linux/amd64`, `linux/arm64`,
`darwin/arm64`, and `darwin/amd64`, asserts the installer printed `Checksum OK`, and asserts
`ironctl version` reports the exact tag. It is **fail-closed**: a failure turns the whole
Release run red, which also blocks the Image workflow (it chains on Release `success`). A red
smoke run on an already-published release is the signal to **yank** (the assets are out by
that point). This gate lands with IRO-15.

---

## 4. How to verify a release (user-facing)

This is the procedure a user — or you, post-release — runs to prove a release is trustworthy.
It is also documented in the README's *Verifying a release* section. The trust chain is:
**cosign signature → `SHA256SUMS` → your archive**, plus an independent provenance attestation.

Download the archive for your platform, plus `SHA256SUMS`, `SHA256SUMS.sig`, and
`SHA256SUMS.pem` from the release.

**Step 1 — verify the signature over `SHA256SUMS` (keyless cosign; no key to manage).**
The signing identity is the release workflow's OIDC identity, not a long-lived key:

```sh
cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \
  --certificate-identity-regexp '^https://github.com/IronSecCo/ironclaw/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

This proves `SHA256SUMS` was produced by the IronClaw release workflow and not tampered with.

**Step 2 — verify your archive against the (now-trusted) checksum file:**

```sh
sha256sum -c SHA256SUMS        # macOS: shasum -a 256 -c SHA256SUMS
```

**Step 3 — verify build provenance (ties the artifact to the source commit + workflow):**

```sh
# the downloaded archive:
gh attestation verify ironclaw_<ver>_<os>_<arch>.tar.gz --repo IronSecCo/ironclaw
# a binary extracted from it (e.g. after `tar xzf`):
gh attestation verify ./ironctl --repo IronSecCo/ironclaw
# and the container image:
gh attestation verify oci://ghcr.io/ironsecco/ironclaw-controlplane:<tag> --repo IronSecCo/ironclaw
```

**The installers verify automatically.** `scripts/install.sh` and `scripts/install.ps1`
download `SHA256SUMS` and refuse to install on a checksum mismatch (`install.sh` prints
`Checksum OK` on success and `die`s on mismatch; `install.ps1` `throw`s on mismatch). The
installers do **not** perform the cosign/attestation steps — run those manually (Steps 1 & 3)
when you need full supply-chain assurance beyond the checksum.

---

## 4a. Reproducing the published Linux binaries from source (IRO-127)

The strongest guarantee a reproducible-builds reviewer wants is to **rebuild from the source
commit and get bit-for-bit the same artifacts** the release published — proving the binaries
contain nothing that isn't in the public source. IronClaw's `linux/amd64` and `linux/arm64`
artifacts are reproducible this way because the release builds them inside a **digest-pinned
container** (`golang:1.23.12-bookworm`), so the Go compiler/linker **and** the gcc/glibc that
compile the vendored SQLCipher C amalgamation are byte-identical for every rebuilder — not
whatever patch a given `ubuntu-latest` runner happened to ship. Same commit → same toolchain →
same bytes.

> **Scope.** Only the Linux targets carry this guarantee. macOS and Windows archives are built
> natively on the host (different SDK / `zip` implementation) and are **not** bit-reproducible
> across machines — verify those via the cosign signature and provenance attestation (Section 4)
> instead.

The builder image is pinned by digest in **one place each** in `release.yml` (the `build` job's
`container:`) and `reproducibility.yml` (the `verify` job's `container:`). To roll the toolchain,
bump both to the new `golang:<ver>-bookworm@sha256:<digest>` and update the command below.

**To reproduce `linux/amd64` (use `linux/arm64` by swapping `GOARCH` + adding the cross-gcc):**

```sh
# 1. Check out the EXACT released commit (the tag points at it).
git clone https://github.com/IronSecCo/ironclaw && cd ironclaw
git checkout v0.1.<n>          # the release tag you are verifying
TAG=v0.1.<n>

# 2. Build the binaries inside the same digest-pinned container the release used.
docker run --rm -v "$PWD":/src -w /src \
  golang:1.23.12-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db \
  bash -c '
    set -euo pipefail
    git config --global --add safe.directory /src   # root-in-container reads the mounted .git
    export CGO_ENABLED=1 GOMAXPROCS=1 GOOS=linux GOARCH=amd64
    go mod download all
    ldflags="-s -w -buildid= -X github.com/IronSecCo/ironclaw/internal/version.Version='"$TAG"' -extldflags=-Wl,--build-id=none"
    mkdir -p dist/pkg
    for c in controlplane ironctl sandbox; do
      out=ironctl; [ "$c" = controlplane ] && out=ironclaw-controlplane; [ "$c" = sandbox ] && out=ironclaw-sandbox
      go build -trimpath -ldflags "$ldflags" -o "dist/pkg/$out" "./cmd/$c"
    done
    cp LICENSE README.md dist/pkg/
    # Deterministic archive (fixed mtime from the commit, sorted, numeric-0 owner, gzip -n):
    SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
    tar --sort=name --mtime="@${SOURCE_DATE_EPOCH}" --owner=0 --group=0 --numeric-owner \
        -cf - -C dist/pkg . | gzip -n > "ironclaw_${TAG#v}_linux_amd64.tar.gz"
    sha256sum "ironclaw_${TAG#v}_linux_amd64.tar.gz" dist/pkg/iron*'
```

**Compare to the release.** The printed archive hash must equal the `linux_amd64` row of the
published `SHA256SUMS`, and each printed binary hash must equal what `gh attestation verify
./<binary> --repo IronSecCo/ironclaw` attests. A match proves the published `linux/amd64` set is
reproducible from source; a mismatch means either you built a different commit/tag or a
non-pinned input crept into the release — investigate before trusting it.

**This is enforced in CI.** `reproducibility.yml` builds all three commands twice inside the
pinned container on **two different host runner images** (`ubuntu-22.04` + `ubuntu-24.04`) and
fails the build unless the binaries **and** the deterministic archive are byte-identical across
hosts — i.e. it tests cross-machine reproducibility on every PR and weekly, not just
same-runner determinism.

---

## 5. Partial-failure semantics (operator decision)

The release job uploads the binaries + `SHA256SUMS` **first**, then attaches SBOMs, the cosign
signature, and attestations. This ordering guarantees a tooling outage can't withhold the
binaries — but it means a failure *after* publish can leave a release that is **published but
not fully signed/attested**. That is not a verifiable release.

If a Release run goes red **after** the `Publish release` step:

1. Check which post-publish step failed (`gh run view <run-id> --log-failed`).
2. **Re-run the failed job** (`gh run rerun <run-id> --failed`). The signing/SBOM/attest steps
   `--clobber` their uploads, so re-running is safe and idempotent and will complete the set.
3. If re-running cannot complete the signature/attestation (e.g. Sigstore is down for an
   extended window), **yank the release** rather than leave an unverifiable set published.
   Do not advertise or chain an image off a partially-signed release.

Never hand-sign or hand-upload a `SHA256SUMS.sig` from a local key — signing is keyless/OIDC
by design; there is no long-lived signing secret. If you encounter one, stop and escalate.

---

## 6. Yanking a release

Yank when a published release is bad: a smoke failure, a broken/partially-signed artifact, a
critical defect, or a wrong tag. Prefer a yanked release over a misleading green one.

```sh
TAG=v0.1.123          # the bad tag

# 1. Delete the GitHub Release AND its tag (so install.sh can no longer resolve it).
gh release delete "$TAG" --yes --cleanup-tag
#    (equivalently: gh release delete "$TAG" --yes && git push origin ":refs/tags/$TAG")

# 2. If the yanked release was marked --latest, repoint "latest" to the last good release
#    so `install.sh` (default: latest) stops serving the bad one.
gh release edit <previous-good-tag> --latest

# 3. Remove or repoint the GHCR image tags built from the bad commit.
#    Delete the version tag, and repoint :latest to the last good image if needed.
#    (GHCR package versions are managed under the repo owner's Packages settings / API.)
gh api -X DELETE "/orgs/IronSecCo/packages/container/ironclaw-controlplane/versions/<version-id>"
```

Notes:

- Deleting the tag frees the `v0.1.<count>` number **only for the same commit** (see §2). To
  ship a fix, push the fix to `main`; the commit count increments and a fresh tag is cut. To
  re-cut the *same* commit after fixing tooling (not code), delete the tag and re-run Release —
  `exists` is `false` again and it rebuilds cleanly.
- `install.sh`/`install.ps1` default to the GitHub *latest* release, so Step 2 is what actually
  stops new installs of a yanked build. Users who pinned `IRONCLAW_VERSION=<bad tag>` will get a
  clean "no asset / release not found" error once the release is deleted — which is the intended
  fail-closed behavior.
- Announce the yank (and the replacement tag) wherever releases are tracked, and record it on
  the triggering issue.

---

## 7. Pausing & resuming the pipeline

If prebuilt releases need to be **paused** (the README may carry a "paused" banner directing
users to build from source), pause/resume cleanly without deleting the workflow:

```sh
gh workflow disable release.yml     # stop auto-cutting releases on push to main
gh workflow disable image.yml       # (optional) also stop image publishes
# ...resume:
gh workflow enable release.yml
gh workflow enable image.yml
```

When the pipeline is paused, the README directs users to **build from source**. When you
resume, update the README's *Installation* / *Verifying a release* notes to drop the "paused"
banners so the one-liner install path is advertised again.

---

## 8. Required status checks & branch protection

`main` is intended to be protected by enforced required checks. The spec lives at
`.github/rulesets/main.json` (`build` + `CodeQL` required, linear history, signed commits,
no force-push/deletion). Applying that file as an *active* GitHub ruleset is tracked
separately (IRO-14); ratcheting protection **up** (more enforced checks)
is the default direction. Never relax or disable a required check to unblock a merge — fix the
check on its own ticket instead.

---

## 9. Quick command reference

```sh
# Watch the current release
gh run list --workflow=release.yml --limit 5
gh run watch <run-id>

# Manually cut / re-cut
gh workflow run release.yml                 # auto-derive tag
gh workflow run release.yml -f version=v0.1.99

# Re-run a failed (post-publish) release job — idempotent
gh run rerun <run-id> --failed

# Inspect a release
gh release view <tag>
gh release view <tag> --json assets -q '.assets[].name'

# Yank
gh release delete <tag> --yes --cleanup-tag
gh release edit <previous-good-tag> --latest

# Verify (user path)
cosign verify-blob SHA256SUMS --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \
  --certificate-identity-regexp '^https://github.com/IronSecCo/ironclaw/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum -c SHA256SUMS
gh attestation verify ironclaw_<ver>_<os>_<arch>.tar.gz --repo IronSecCo/ironclaw
gh attestation verify ./ironctl --repo IronSecCo/ironclaw   # extracted binary
```

---

*Related tickets:* pipeline handoff IRO-12; this runbook
IRO-16; arm64 image IRO-13; ruleset enforcement
IRO-14; release smoke test IRO-15.
