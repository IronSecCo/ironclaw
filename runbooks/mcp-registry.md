# Publishing IronClaw to the official MCP Registry

IronClaw is listed on the official MCP Registry
(`registry.modelcontextprotocol.io`) under the account-free namespace
**`io.github.IronSecCo/ironclaw`**. This runbook covers how the listing is
authored, how it is published reproducibly from a tagged release, and how a user
verifies it.

## Trust model (why this is account-free)

The `io.github.*` namespace on the official registry is authenticated by the
**owning GitHub org's Actions OIDC token** — no interactive account, no
long-lived secret. Because the `publish-mcp-registry` job runs in
`IronSecCo/ironclaw`, GitHub issues it an OIDC token whose subject proves the
job belongs to the `IronSecCo` org, and the registry binds the
`io.github.IronSecCo/*` namespace to it. The only permission the job holds is
`id-token: write` (plus `contents: read` to check out `server.json`).

## What the listing points at

The listed package is **`ghcr.io/ironsecco/ironclaw-mcp`** — the slim,
socket-free thin client from `container/mcp.Dockerfile`. It is a static `ironctl`
on `distroless/static:nonroot` with no shell, no package manager, no `runsc`, and
no host Docker socket. `sandbox_exec` delegates every box to a running IronClaw
control-plane over its authenticated API; the control-plane owns the hardened
gVisor launch. The MCP server itself holds **no host privilege**, and with no
control-plane reachable the tool fails closed rather than falling back to host
Docker.

It is deliberately **not** the control-plane image. Listing that one requires
`-v /var/run/docker.sock:/var/run/docker.sock`, which is effectively host-root;
the CEO rejected it as option A under IRO-391. Exactly one image in this repo
carries the `io.modelcontextprotocol.server.name` label, and it is the MCP image,
so a publish cannot accidentally re-bind the listing to the control-plane.

Consumers get two required environment variables, declared in `server.json` and
forwarded with bare `-e NAME` runtime arguments so no value ever appears in the
listing:

| Variable | Meaning |
| --- | --- |
| `IRONCLAW_CONTROLPLANE_URL` | Base URL of the user's running control-plane. Unset means no backend and `sandbox_exec` fails closed. |
| `IRONCLAW_API_TOKEN` | Bearer token for that control-plane's API (`isSecret: true`). |

## Moving parts

| Artifact | Role |
| --- | --- |
| [`server.json`](https://github.com/IronSecCo/ironclaw/blob/main/server.json) | The listing: name, description, repository, and the OCI package that points at `ghcr.io/ironsecco/ironclaw-mcp`. |
| `LABEL io.modelcontextprotocol.server.name` in `container/mcp.Dockerfile` | Ownership proof. The registry fetches the image and refuses to bind the listing unless this label equals the `server.json` name. |
| `build-mcp` job in `.github/workflows/image.yml` | Builds + pushes the multi-arch MCP image, attests provenance and an SBOM, and asserts the label matches `server.json` on **every** arch. |
| `publish-mcp-registry` job in `.github/workflows/image.yml` | Chains off the build/attest/verify chain and publishes the listing via the pinned `mcp-publisher` CLI. |
| `.github/workflows/mcp-registry-deprecate.yml` | Manual status flip for the whole server record. See the caution below before using it. |
| `.github/workflows/mcp-registry-watch.yml` | Daily read-only liveness guard. Asserts the listing still resolves, is `active` + `isLatest`, and names an anonymously pullable image. See [Standing liveness guard](#standing-liveness-guard). |

The publish job runs **only after** `verify-consumer` proves **both** images are
anonymously pullable and attested, and **only for tagged releases**
(`VERSION != dev`). A failure fails the workflow loudly but never rolls back the
already-published, already-attested images.

`ironclaw-mcp` builds multi-arch from a single runner: `ironctl` does not link
cgo, so the Go toolchain cross-compiles both arches (`FROM --platform=$BUILDPLATFORM`
plus `$TARGETARCH`) with no QEMU. The control-plane image still needs its
per-arch native runners because it links SQLCipher through cgo.

## How the version is bound

`image.yml`'s `prepare` job resolves the release tag this commit carries
(`vX.Y.Z`). The publish job then stamps `server.json` at publish time:

- `.version` = the semver form (`X.Y.Z`, leading `v` stripped).
- `.packages[0].identifier` = `ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z`
  — the **immutable release tag**, never `:latest`, so the listing is
  re-derivable from the tagged commit.

The checked-in `server.json` carries `0.0.0` placeholders; they are overwritten
in-job and never committed back.

## Supply-chain pinning of `mcp-publisher`

The `mcp-publisher` binary is pinned three ways before it is allowed to run
(see the `env:` block in the job):

1. **Version** — `MCP_PUBLISHER_VERSION` (exact release tag, never `latest`).
2. **Digest** — `MCP_PUBLISHER_SHA256` verified with `sha256sum -c`.
3. **Provenance** — `cosign verify-blob` against the release's Sigstore bundle,
   pinned to the registry repo's own release workflow identity
   (`MCP_PUBLISHER_CERT_IDENTITY` / `MCP_PUBLISHER_CERT_ISSUER`).

### Bumping `mcp-publisher`

Update all four `MCP_PUBLISHER_*` env values in lock-step:

```bash
V=v1.7.10   # the new tag
gh release download "$V" --repo modelcontextprotocol/registry \
  -p 'mcp-publisher_linux_amd64.tar.gz' -O mp.tgz
sha256sum mp.tgz                                  # -> MCP_PUBLISHER_SHA256
# Identity is the SAN URI on the archive's .sigstore.json signing cert:
#   https://github.com/modelcontextprotocol/registry/.github/workflows/release.yml@refs/tags/<V>
# -> MCP_PUBLISHER_CERT_IDENTITY (issuer stays token.actions.githubusercontent.com)
```

## Cutting a listing (normal path)

Nothing manual. Push to `main` cuts a release (`v0.1.<commit-count>`); the
`Image` workflow builds + attests both GHCR images, `verify-consumer` proves each
is anonymously pullable, then `publish-mcp-registry` publishes the listing and
post-checks the registry.

After publishing, the job runs two steps that matter:

1. **Activate this version.** `mcp-publisher status --status active <name>
   <semver>`, which is the per-version endpoint
   `PATCH /v0/servers/{name}/versions/{version}/status`. A fresh publish normally
   lands `active` on its own, but the server was deprecated **server-wide** on
   2026-07-07, so the job asserts rather than trusts. It never passes
   `--all-versions` (see below).
2. **Post-check.** Queries the **exact** per-server endpoint,
   `/v0/servers/{urlencoded name}/versions`, and asserts the new version is
   `active` **and** `isLatest`. Two traps it exists to avoid:
   - `?search=` is a fuzzy search, not a name filter.
   - The response nests the record under `.server`, so `.servers[].name` is
     always `null`. Matching on it is what false-failed this job through
     0.1.216–0.1.230 while the publishes themselves were succeeding.

## Relighting a deprecated listing

Publish a new version. The release path activates and verifies it per-version, so
a green `Image` workflow **is** the proof that the listing is live.

The trap is the other direction: a workflow that **never ran** is also never red.
`Image` chains off `Release`, so anything that fails `Release` — including a purely
cosmetic packaging job — silently skips the publish, and every workflow in the repo
stays green while the listing rots. That is exactly how 0.1.230 sat `deprecated` for
three weeks and ~170 releases (IRO-611). So: green `Image` proves the listing is
live; the **absence** of a red `Image` proves nothing. That gap is what
[`mcp-registry-watch.yml`](#standing-liveness-guard) covers.

When the release path is broken and the listing needs relighting now, dispatch the
publish by hand:

```bash
# The tag MUST resolve to the commit being built: workflow_dispatch runs the workflow
# file from the ref it builds, and a tag naming a different commit stamps the listing
# with a version that does not describe the image (IRO-625).
gh workflow run image.yml --ref main \
  -f tag=<tag on the current main tip> \
  -f publish_mcp_registry=true
```

Do **not** reach for `mcp-registry-deprecate.yml` with `status=active`, and do not
pass `--all-versions` to `mcp-publisher status`. Both are **server-wide**: they
flip every version in one transaction, including 0.1.216–0.1.230, which name the
control-plane image launched over a host Docker socket. Those stay `deprecated`
permanently — they describe a trust model we tell people not to run (IRO-391 /
IRO-613). The only thing that ever goes `active` is a version built from
`container/mcp.Dockerfile`.

## Package visibility (no gate — verified on first publish)

`ghcr.io/ironsecco/ironclaw-mcp` was **born anonymously pullable**. We expected
the opposite (GHCR historically created every new package private, as
`ironclaw-controlplane` did in June 2026, which needed a manual flip), so IRO-618
pre-staged an org-admin gate for it. The gate never fired. Evidence from the
first release that pushed the package — `Image` run
[30408078319](https://github.com/IronSecCo/ironclaw/actions/runs/30408078319),
`v0.1.403`, the first `build-mcp` on `main`:

```
Anonymous manifest pull: ghcr.io/ironsecco/ironclaw-mcp:latest
  -> HTTP 200 (anonymously pullable)
Anonymous manifest pull: ghcr.io/ironsecco/ironclaw-mcp:v0.1.403
  -> HTTP 200 (anonymously pullable)
```

Zero retries, and no human touched the package between the push (23:28 UTC) and
`verify-consumer` (23:30 UTC), so this was not a flip we made and forgot. The
likely reason is that GHCR now links a `GITHUB_TOKEN`-pushed package to the
repository that pushed it and inherits that repository's visibility, and
`IronSecCo/ironclaw` is public. Treat that as the probable mechanism, not a
guarantee: it is GitHub-side behaviour we do not control and did not always get.

**So do not pre-emptively flip anything.** The claim that matters is checked on
every release by `verify-consumer`, which pulls anonymously with no registry
credentials. If it ever goes red on the token request or the manifest fetch, the
package is private (or was never pushed), and the fix is still the one-time
org-admin action:

> IronSecCo → Packages → `ironclaw-mcp` → Package settings → Change visibility →
> Public

then re-run the `Image` workflow (or let the next release run it). This is a
package-visibility change, not a pipeline change; no workflow edit is involved.
`GITHUB_TOKEN` cannot do it: there is no REST endpoint for container-package
visibility, it is a UI action.

Anyone can re-check the current state without credentials. Note the **absence of
`curl -f`** on the token request: GHCR denies the token request *itself* with 403
for a private package, and `-f` turns that into a bare `curl: (56)` with no status
line — losing the one reading you came for. Check the token request's own status
instead:

```bash
repo=ironsecco/ironclaw-mcp
code="$(curl -sS -o /tmp/tok.json -w '%{http_code}' \
  "https://ghcr.io/token?service=ghcr.io&scope=repository:${repo}:pull")"
if [ "${code}" != "200" ]; then
  echo "token request -> HTTP ${code}: package is PRIVATE, or was never pushed"
else
  curl -sS -o /dev/null -w 'manifest -> HTTP %{http_code}\n' \
    -H "Authorization: Bearer $(jq -r .token </tmp/tok.json)" \
    -H 'Accept: application/vnd.oci.image.index.v1+json' \
    "https://ghcr.io/v2/${repo}/manifests/latest"   # 200 = public
fi
```

### Reading a red `verify-consumer`

There is **no expected-red step anywhere in this pipeline** — both packages are
public and this job passes for both. So a red is a real failure: diagnose it,
never wave it through as a known gate. Visibility is only one of four causes, and
not the most serious. In rough order of likelihood:

1. **Propagation lag.** GHCR is eventually consistent right after a push. The job
   already retries 5x/10s on both the token request and the manifest fetch, so a
   red means it stayed broken for about a minute.
2. **Digest mismatch** — the tag resolves, but not to the index this run attested.
   This is the security-relevant one: consumers would pull something we never
   attested. Treat it as a clobbered or racing tag and stop the release. Check the
   pipeline before you go looking for a clobbering third party: the first time this
   ever fired, we were the ones publishing the mismatch (IRO-629, below).
3. **Attestation not verifiable from outside** — the provenance/SBOM upload
   failed, or something other than this workflow pushed the image.
4. **The package really is private**, per the section above. Because GHCR returns
   the identical 403 for private *and* never-pushed, settle which one before
   asking an admin for anything: `gh api /orgs/IronSecCo/packages/container/<pkg>`
   answers **404 "Package not found"** when it does not exist, and **200** — or
   **403 "needs `read:packages` scope"** if your token lacks the scope — when it
   does. Any non-404 answer proves existence, which is the only bit you need.

### v0.1.411: when the mismatch was ours (IRO-629)

`Image` run [30412119358](https://github.com/IronSecCo/ironclaw/actions/runs/30412119358)
went red on cause 2, and the mismatch was real — but nothing external had touched a
tag. The pipeline published it.

IRO-625 made `:latest` **conditional**: it moves forwards only, so a build of a
commit that is no longer the `main` tip publishes its immutable `:<version>` alone.
That run was exactly that case (`3c87a8a` had fallen behind `45b3ae9` in the six
minutes between `Release` finishing and `Image` starting). Two steps had not been
told: `merge` read the published index digest back from a hardcoded
`imagetools inspect "${IMAGE}:latest"`, and `verify-consumer` asserted `:latest`
unconditionally.

The red was the harmless half. `merge`'s digest is the **subject** of everything
downstream, so reading it from a tag this run did not publish meant the run:

- attached the SLSA provenance **and** the CycloneDX SBOM to `sha256:be66da22…` —
  an older index it had not built, stamping it with a build it did not come from;
- left the image it actually built, `ironclaw-controlplane:v0.1.411`
  (`sha256:19a65817…`), with **no provenance and no SBOM at all**.

`verify-consumer` then compared `:latest` (the wrong index, which of course matched
the wrong digest it had been handed) and `:v0.1.411` (the real one, which did not).
The job failing is the only reason `publish-mcp-registry` did not go on to stamp an
unattested image ref into an immutable registry version.

**Disposition.** `ironclaw-controlplane:v0.1.411` is **withdrawn, not rebuilt**, for
the same reason as `:v0.1.403` (IRO-625): `workflow_dispatch` runs the workflow file
from the ref it builds, so a rebuild at tag `v0.1.411` would re-run the very code
that caused this. It is superseded by `v0.1.413`, the first release cut from `main`
after the fix. `ironclaw-mcp:v0.1.411` is intact — `build-mcp` pushes its tags in one
`build-push-action` call and reports that build's own digest, so it never had a
`:latest` lookup to get wrong. The MCP listing was never touched.

> **What that supersession does and does not do.** It moves `:latest` off
> `be66da22` onto the index `v0.1.413` actually built. It does **not** remove the
> false provenance statement, and no later release ever will. See
> [What could not be retracted](#what-could-not-be-retracted) — do not read
> "superseded" as "cleaned up".

**The rule this leaves behind:** nothing in `image.yml` may name `latest`. `prepare`
emits a `tags` output holding exactly the tag names the run publishes, immutable
`:<version>` first, and `merge` and `verify-consumer` both derive from it. `merge`
additionally asserts that *every* tag it applied resolves to the one index it is
about to attest, so a digest it did not build can no longer become an attestation
subject silently — it fails the release instead. More generally: when a publish step
becomes conditional, every step that reads the result back has to learn the
condition, or it will keep reading a stale answer and call it success.

### `sha256:be66da22` carries two green provenance statements, permanently

This is the part that outlives the fix, so read it before you trust a green
`gh attestation verify` on this package.

`ghcr.io/ironsecco/ironclaw-controlplane@sha256:be66da22455e11b8625693a29535f599500c126165eeabb34775492a962ce5b7`
is the index legitimately built and published as **`:v0.1.407`**. It now carries
**two** SLSA v1 provenance statements, and **both verify green** — a green verify is
therefore *not* sufficient to establish where this image came from:

| Statement | `invocationId` run | `resolvedDependencies` gitCommit | Verdict |
| --- | --- | --- | --- |
| A | [30409777856](https://github.com/IronSecCo/ironclaw/actions/runs/30409777856) | `405dcfb3` | **legitimate** — this run built the index |
| B | [30412119358](https://github.com/IronSecCo/ironclaw/actions/runs/30412119358) | `45b3ae98` | **false** — this is the `v0.1.411` run, which built `sha256:19a65817…` |

Statement B is the misattribution described above. Note that its gitCommit
`45b3ae98` is not the commit that run *built* either (`3c87a8a`, the commit `Release`
handed it) — it is the `main` tip that `image.yml` itself checked out for a
`workflow_run` event. So the commit field alone does not identify a build; do not
use it as one.

#### Telling them apart

Two independent checks, both runnable by an outside auditor with no access to
anything of ours. Neither needs our logs.

**1. Immutable version-tag correspondence (primary).** Every Image run publishes an
immutable `:<version>` tag naming the index it built. Resolve it and compare to the
statement's subject. The run that built the subject must have a `:<version>` that
resolves to that subject:

```bash
IMAGE=ghcr.io/ironsecco/ironclaw-controlplane
SUBJECT=sha256:be66da22455e11b8625693a29535f599500c126165eeabb34775492a962ce5b7

# List every statement bound to this digest and the run that claims it.
# NOTE: `gh attestation verify` prints NOTHING on success (gh 2.95.0) and exits 0 if ANY
# statement verifies, so the exit code cannot tell you there are two. Always read the JSON.
gh attestation verify "oci://${IMAGE}@${SUBJECT}" --repo IronSecCo/ironclaw --format json \
  | jq -r '.[].verificationResult.statement.predicate.runDetails.metadata.invocationId'
# => .../runs/30412119358/attempts/1
# => .../runs/30409777856/attempts/1
# (The id is at runDetails.metadata.invocationId. `runDetails.invocation.id` does not
#  exist and yields null, which reads as "unclaimed" rather than as a wrong jq path.)

# Resolve the version tag each of those runs published.
resolve() {
  tok="$(curl -sS "https://ghcr.io/token?service=ghcr.io&scope=repository:ironsecco/ironclaw-controlplane:pull" | jq -r .token)"
  curl -sSI -H "Authorization: Bearer ${tok}" \
    -H 'Accept: application/vnd.oci.image.index.v1+json' \
    "https://ghcr.io/v2/ironsecco/ironclaw-controlplane/manifests/$1" \
    | tr -d '\r' | awk 'tolower($1)=="docker-content-digest:"{print $2}'
}
resolve v0.1.407   # => sha256:be66da22...  == SUBJECT  -> statement A is real
resolve v0.1.411   # => sha256:19a65817...  != SUBJECT  -> statement B is false
```

**2. Build time versus run window (corroborating, artifact-side only).** The index's
per-platform image config carries a `created` timestamp. A run that started *after*
an artifact already existed cannot have built it:

- `be66da22` (amd64 config) `created` = **2026-07-29T00:01:44Z**
- run 30409777856 ran 00:00:21Z -> 00:03:06Z — the build falls inside it. Consistent.
- run 30412119358 ran 00:46:08Z -> 00:48:43Z — **44 minutes after** the image already
  existed. Statement B is impossible on its face.

Read the timestamp with:

```bash
docker buildx imagetools inspect "${IMAGE}@${SUBJECT}" \
  --format '{{ range $p, $img := .Image }}{{ $p }} {{ $img.Created }}
{{ end }}'
```

**Known gap.** The control-plane image carries no
`org.opencontainers.image.revision` label (only `ironclaw-mcp` has an asserted
label, and it is the MCP Registry ownership one). If it did, a third and fully
self-contained check would exist: compare the label to each statement's
`resolvedDependencies` gitCommit. Tracked as a follow-up; until then use checks 1
and 2.

#### The same check on a healthy release, for contrast

`v0.1.413` (`Image` run
[30423817523](https://github.com/IronSecCo/ironclaw/actions/runs/30423817523), the
first release cut from `main` after the fix) is what the check is supposed to look
like. Verified anonymously from outside the pipeline:

- `merge` published one index, `sha256:00afb76e…`, as exactly `v0.1.413 latest` —
  immutable version first — and its per-tag loop confirmed both tags resolve to it
  before anything was attested.
- `:latest` and `:v0.1.413` both resolve to `sha256:00afb76e…`.
- `gh attestation verify` on `:latest` returns **one** statement, from run
  `30423817523`, subject `sha256:00afb76e…`, gitCommit `33ee5122` — the run that
  built it, the index it built, the commit it built from. The CycloneDX SBOM is
  bound to the same subject.

`statements: 1` plus a subject that matches the run's own `:<version>` is the
healthy shape. `statements: 2` on `be66da22` is the anomaly.

### What could not be retracted

Stated plainly so nobody later reads this entry as a cleanup report:

- **The false provenance statement on `be66da22` is permanent.** Attestations are
  keyed by digest and are append-only, and the backing Rekor transparency-log entry
  is immutable. There is no delete, no revoke, and no supersede. Statement B will
  verify green against that digest forever.
- **Cutting `v0.1.413` did not change that.** It moved the `:latest` *tag*, which is
  the whole of the consumer-facing remediation. The *attestation* is bound to the
  digest, not the tag, so tag moves are invisible to it.
- **Deleting the GHCR package version would not change it either.** Deleting the
  version removes our copy of the image; it does not retract the statement. Anyone
  holding or re-pushing that exact index elsewhere still gets a green verify on both
  statements. Do not delete it and call the record corrected.

**Disposition of the `be66da22` package version: retained, deliberately.** Now that
`:latest` has moved to `sha256:00afb76e…`, the argument for deleting it is gone and
the arguments against it are not:

- It is **not a bad artifact**. It is the legitimate `v0.1.407` build, still
  correctly attested by statement A. The defect is a spurious *second* claim about
  it, not the image.
- `v0.1.407` is a published GitHub Release and the version the Homebrew formula
  tracked. Deleting the image would break anyone pinned to it, to fix nothing.
- Deletion destroys the evidence. The two statements are the only artifact against
  which the check above can be demonstrated; a future auditor who finds this entry
  should be able to reproduce it.

So: keep the version, keep `:v0.1.407` pointing at it, and rely on this entry plus
the subject-correspondence check rather than on removal. Removal was never available
as a remedy — it only ever looked like one.
- **`ironclaw-controlplane:v0.1.411` (`sha256:19a65817…`) has no provenance and no
  SBOM at all**, and will not get any: attesting it now would require a run at that
  ref, which re-executes the buggy workflow. It is withdrawn. Treat an unattested
  `v0.1.411` as unverifiable, which is the correct outcome — the verification path
  fails closed on it (HTTP 404, no statement to verify).

The remediation that *did* land is: `:latest` no longer resolves to `be66da22`, the
pipeline can no longer attest a digest it did not build (`merge` asserts subject
correspondence and nothing names `latest`), and this entry exists so that a green
verify on `be66da22` is read with the check above rather than at face value.

## Standing liveness guard

`.github/workflows/mcp-registry-watch.yml` runs daily (and on demand) and asserts,
from the outside and with no credentials, the three things a real MCP client needs:

1. `io.github.IronSecCo/ironclaw` resolves on the registry.
2. Its **`isLatest`** version is `active`. Clients resolve the latest version, so an
   `active` old version sitting behind a `deprecated` latest is still a dark listing.
3. The OCI image that version names is **anonymously pullable** from GHCR. A healthy
   registry record pointing at a private or deleted image is dark for every consumer
   and for the registry's own OCI validator.

Any of those failing is a real outage of our only MCP directory presence and fails
the run. It is read-only — no registry auth, no GHCR auth, no publish — so it can
report the outage but never repair it; relighting stays a deliberate operator action
(above).

**Version drift is a warning, not a failure.** When the listing trails the newest
release tag, the run warns and stays green. Drift is expected whenever the
`Release → Image` chain is broken (IRO-621), and a guard that goes red every day for
a known reason is a guard people learn to ignore — the same misleading-signal
pathology this workflow exists to kill. If the chain is green and the drift warning
persists, the publish job is being skipped and needs investigating.

## Yanking / superseding a listing

The registry has no destructive delete for publishers; you supersede or
deprecate:

- **Supersede** — publish a newer `version`. The registry marks the newest as
  `isLatest: true`; older versions remain queryable but drop out of the default
  view.
- **Deprecate one version** (preferred) — `mcp-publisher status --status
  deprecated <name> <semver>`. Per-version, so it cannot touch anything else.
- **Deprecate every version** — `mcp-registry-deprecate.yml`, or `mcp-publisher
  status --all-versions`. **Server-wide, break-glass only.** Safe in the
  `deprecated` direction; never run it with `status=active`.

Clients surface deprecated servers with a warning instead of hiding them. Note
`statusMessage` is rejected when `status=active`, so send the status alone.

If a bad image was published, cut a fixed release: the new listing repoints at
the new immutable image tag; the bad tag can be separately yanked from GHCR.

## How a user verifies the listing

```bash
# 1. The listing resolves, is active, and points at the socket-free image:
curl -s "https://registry.modelcontextprotocol.io/v0/servers/io.github.IronSecCo%2Fironclaw/versions" \
  | jq '.servers[] | {v: .server.version,
                      pkg: .server.packages[0].identifier,
                      status: ._meta["io.modelcontextprotocol.registry/official"].status}'

# 2. The image the listing names carries build provenance tying it to this repo:
gh attestation verify oci://ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z \
  --repo IronSecCo/ironclaw

# 3. It really is the thin client — no docker socket, no host privilege:
IRONCLAW_CONTROLPLANE_URL=http://127.0.0.1:8787 IRONCLAW_API_TOKEN=<token> \
  docker run --rm -i -e IRONCLAW_CONTROLPLANE_URL -e IRONCLAW_API_TOKEN \
  ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z
# stderr: "thin-client backend -> control-plane ... (this process holds no host privilege)"
```

## Why OCI, and why the thin client (settled — IRO-391 / IRO-414 / IRO-612)

The registry accepts packages only from supported package registries
(npm, PyPI, OCI, MCPB, NuGet, Cargo). IronClaw ships via Homebrew, `curl | sh`,
and the GHCR image — of these, **only the GHCR image is a registry-supported
package type**, so the listing uses the OCI form. Native-only
(`command: ironctl, args: [mcp, serve]`, per `docs/mcp-server/`) remains the flow
we document and recommend, but it is not a registry-supported package type.

The original listing took the shortest path to a working `docker run`: the
control-plane image plus a host Docker-socket mount, so `ironctl mcp serve` could
spawn sibling gVisor containers. That is effectively host-root and contradicts
IronClaw's hardening promise, so the CEO rejected it (option A) and those
versions were deprecated.

The shipped answer is the **thin client**: IRO-414 split the box lifecycle out to
the control-plane's authenticated `POST /v1/sandbox/exec`, which lets the listed
image drop every host privilege. IRO-612 repointed `server.json` at it, moved the
ownership label onto it, added the `build-mcp` job that actually publishes it, and
put the release path back in charge of the listing.
