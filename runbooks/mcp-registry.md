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
a green `Image` workflow **is** the proof that the listing is live. There is no
manual step.

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
   attested. Treat it as a clobbered or racing tag and stop the release.
3. **Attestation not verifiable from outside** — the provenance/SBOM upload
   failed, or something other than this workflow pushed the image.
4. **The package really is private**, per the section above. Because GHCR returns
   the identical 403 for private *and* never-pushed, settle which one before
   asking an admin for anything: `gh api /orgs/IronSecCo/packages/container/<pkg>`
   answers **404 "Package not found"** when it does not exist, and **200** — or
   **403 "needs `read:packages` scope"** if your token lacks the scope — when it
   does. Any non-404 answer proves existence, which is the only bit you need.

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
