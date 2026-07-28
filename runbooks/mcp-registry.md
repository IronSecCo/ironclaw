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

## What the listing actually launches

The listed artifact is **`ghcr.io/ironsecco/ironclaw-mcp`**
(`container/mcp.Dockerfile`) — the slim, socket-free MCP server: distroless
static, nonroot (65532), no shell, no package manager, and **no Docker socket
mount**. `sandbox_exec` delegates every box to a running IronClaw control-plane
over its authenticated API, so the container an MCP client starts holds no host
privilege. With no control-plane configured it errors; it never falls back to
host Docker.

It is deliberately **not** `ironclaw-controlplane`. That image owns the gVisor
box lifecycle and therefore needs host Docker access; listing it would have made
`-v /var/run/docker.sock:/var/run/docker.sock` part of the published launch
line, i.e. host-root by default. That trade-off was rejected on IRO-391, and the
versions published under it (`0.1.216`–`0.1.230`) remain `deprecated` on the
registry on purpose.

Because the listed image is privilege-free, `server.json` carries no
`--entrypoint` override and no `packageArguments`: the image's own entrypoint is
already `ironctl mcp serve`. The full client launch line is just

```bash
docker run --rm -i \
  -e IRONCLAW_CONTROLPLANE_URL=http://127.0.0.1:8787 \
  -e IRONCLAW_API_TOKEN=<token> \
  ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z
```

## Moving parts

| Artifact | Role |
| --- | --- |
| [`server.json`](https://github.com/IronSecCo/ironclaw/blob/main/server.json) | The listing: name, description, repository, and the OCI package that points at `ghcr.io/ironsecco/ironclaw-mcp`. |
| `LABEL io.modelcontextprotocol.server.name` in `container/mcp.Dockerfile` | Ownership proof. The registry fetches the image and refuses to bind the listing unless this label equals the `server.json` name. Keep the two in lock-step. |
| `build` / `merge` / `verify-consumer` jobs in `.github/workflows/image.yml` | Build both images multi-arch, attest them, and prove they are anonymously pullable. The image set is a matrix resolved by `prepare`. |
| `publish-mcp-registry` job in `.github/workflows/image.yml` | Chains off that verify chain and publishes the listing via the pinned `mcp-publisher` CLI, then asserts it is live. |
| `.github/workflows/mcp-registry-deprecate.yml` | Manual, server-wide status flip (`deprecated` / `active` / `deleted`) for **all** versions. Break-glass only. |

The publish job runs **only after** `verify-consumer` proves the image is
anonymously pullable and attested, **only when this commit carries
`container/mcp.Dockerfile`** (`mcp_present`), and **only for tagged releases**
(`VERSION != dev`). A failure fails the workflow loudly but never rolls back the
already-published, already-attested image.

## How the version is bound

`image.yml`'s `prepare` job resolves the release tag this commit carries
(`vX.Y.Z`). The publish job then stamps `server.json` at publish time:

- `.version` = the semver form (`X.Y.Z`, leading `v` stripped).
- `.packages[0].identifier` = `ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z`
  — the **immutable release tag**, never `:latest`, so the listing is
  re-derivable from the tagged commit.

The checked-in `server.json` carries `0.0.0` placeholders; they are overwritten
in-job and never committed back.

Validate a stamped `server.json` without publishing anything:

```bash
jq '.version="0.1.400" | .packages[0].identifier="ghcr.io/ironsecco/ironclaw-mcp:v0.1.400"' server.json \
  | curl -sS -X POST https://registry.modelcontextprotocol.io/v0/validate \
      -H 'Content-Type: application/json' -d @- | jq .
# -> {"valid": true, "issues": []}
```

Note the registry caps `description` at **100 characters** server-side.

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
`Image` workflow builds + attests both GHCR images, `verify-consumer` proves
they are anonymously pullable, then `publish-mcp-registry` publishes the
listing, sets that version `active`, and post-checks that it is live.

## Checking the listing state

Always query by **exact name**, not `?search=`:

```bash
NAME=$(jq -rn --arg s 'io.github.IronSecCo/ironclaw' '$s|@uri')
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${NAME}/versions" \
  | jq '[.servers[] | {
      version:  .server.version,
      package:  .server.packages[0].identifier,
      status:   ._meta["io.modelcontextprotocol.registry/official"].status,
      isLatest: ._meta["io.modelcontextprotocol.registry/official"].isLatest }]'
```

Two traps live here, and together they are how a dark listing went unnoticed for
three weeks (IRO-611):

- `?search=` is a **fuzzy** match, not a name filter. A post-check written
  against it can silently match nothing and false-fail a publish that actually
  succeeded.
- Result entries are shaped `{server, _meta}` — the name and version live at
  `.server.name` / `.server.version`, **not** at the top level. `.servers[].name`
  is always `null`.

A green `Image` workflow is not evidence the listing is live. `status` must be
`active` and `isLatest` must be `true`.

## Status: per-version vs. server-wide

The registry has no destructive delete for publishers; you supersede or change
lifecycle status.

- **Supersede** — publish a newer `version`. The newest becomes `isLatest`.
- **Per-version status** —
  `PATCH /v0/servers/{name}/versions/{version}/status`. This is what
  `publish-mcp-registry` uses to assert the version it just published is
  `active`, and it is the right tool when only some versions are bad.
- **Server-wide status** — `PATCH /v0/servers/{name}/status` flips **every**
  version in one transaction. That is what
  `.github/workflows/mcp-registry-deprecate.yml` does; it is break-glass only.
  Running it with `status=active` would resurrect the deprecated
  `0.1.216`–`0.1.230` socket-mount versions, which must stay deprecated.

The API **rejects `statusMessage` when `status=active`** — send `{"status":
"active"}` alone.

## First publish of a new image (one-time manual gate)

GHCR creates a brand-new package as **private**, and `GITHUB_TOKEN` cannot
change that. The first release that pushes `ghcr.io/ironsecco/ironclaw-mcp` will
therefore fail in `verify-consumer` on the anonymous pull — correctly, since a
private image is unusable by the clients the listing serves, and the registry's
own OCI validator could not fetch it either.

Unblock it once, as an org admin:

> IronSecCo → Packages → `ironclaw-mcp` → Package settings → Change visibility →
> Public

then re-run the `Image` workflow (or let the next release run it). This is a
package-visibility change, not a pipeline change; no workflow edit is involved.

## How a user verifies the listing

```bash
# 1. The listing resolves, is active, and names our slim image:
NAME=$(jq -rn --arg s 'io.github.IronSecCo/ironclaw' '$s|@uri')
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${NAME}/versions" | jq .

# 2. The image the listing names carries build provenance tying it to this repo:
gh attestation verify oci://ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z \
  --repo IronSecCo/ironclaw

# 3. ...and the ownership label the registry validated:
docker buildx imagetools inspect ghcr.io/ironsecco/ironclaw-mcp:vX.Y.Z --raw
```

## Design decision (settled — IRO-391 / IRO-414 / IRO-611)

The registry accepts packages only from supported package registries (npm, PyPI,
OCI, MCPB, NuGet, Cargo). IronClaw ships via Homebrew, `curl | sh`, and GHCR
images — of these, only an OCI image is a registry-supported package type, so
the listing uses the OCI form. The options considered were:

- **A. Control-plane image + host Docker socket.** Works via plain `docker run`,
  but the published launch line grants the MCP server control of the host Docker
  daemon. **Rejected** by the CEO on IRO-391; published as `0.1.216`–`0.1.230`
  before the decision and deprecated on 2026-07-07.
- **B. Dedicated slim MCP image** (`container/mcp.Dockerfile`, IRO-414) that
  delegates every box to a control-plane over its authenticated API and holds no
  host privilege. **Chosen, and what ships today.**
- **C. Native-only** (`command: ironctl, args: [mcp, serve]`, per
  `docs/mcp-server/`). Still the flow we recommend in our own docs, but not a
  registry-supported package type, so it cannot be the registry listing.
