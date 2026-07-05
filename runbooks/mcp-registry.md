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

## Moving parts

| Artifact | Role |
| --- | --- |
| [`server.json`](https://github.com/IronSecCo/ironclaw/blob/main/server.json) | The listing: name, description, repository, and the OCI package that points at our GHCR control-plane image. |
| `LABEL io.modelcontextprotocol.server.name` in `container/controlplane.Dockerfile` | Ownership proof. The registry fetches the image and refuses to bind the listing unless this label equals the `server.json` name. |
| `publish-mcp-registry` job in `.github/workflows/image.yml` | Chains off the image build/merge/attest/verify chain and publishes the listing via the pinned `mcp-publisher` CLI. |

The publish job runs **only after** `verify-consumer` proves the image is
anonymously pullable and attested, and **only for tagged releases**
(`VERSION != dev`). A failure fails the workflow loudly but never rolls back the
already-published, already-attested image.

## How the version is bound

`image.yml`'s `prepare` job resolves the release tag this commit carries
(`vX.Y.Z`). The publish job then stamps `server.json` at publish time:

- `.version` = the semver form (`X.Y.Z`, leading `v` stripped).
- `.packages[0].identifier` = `ghcr.io/ironsecco/ironclaw-controlplane:vX.Y.Z`
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

Nothing manual. Push to `main` cuts a release
(`v0.1.<commit-count>`); the `Image` workflow builds + attests the GHCR image,
`verify-consumer` proves it is pullable, then `publish-mcp-registry` publishes
the listing and post-checks that
`registry.modelcontextprotocol.io/v0/servers?search=io.github.IronSecCo/ironclaw`
resolves at the new version.

## Yanking / superseding a listing

The registry has no destructive delete for publishers; you supersede or
deprecate:

- **Supersede** — publish a newer `version`. The registry marks the newest as
  `isLatest: true`; older versions remain queryable but drop out of the default
  view.
- **Deprecate** — set the server's lifecycle status to `deprecated` (registry
  API, authenticated with the same GitHub OIDC/PAT). Clients surface deprecated
  servers with a warning instead of hiding them.

If a bad image was published, cut a fixed release: the new listing repoints at
the new immutable image tag; the bad tag can be separately yanked from GHCR.

## How a user verifies the listing

```bash
# 1. The listing resolves and points at our image + repo:
curl -s "https://registry.modelcontextprotocol.io/v0/servers?search=io.github.IronSecCo/ironclaw" | jq .

# 2. The image the listing names carries build provenance tying it to this repo:
gh attestation verify oci://ghcr.io/ironsecco/ironclaw-controlplane:vX.Y.Z \
  --repo IronSecCo/ironclaw
```

## Open design decision (CEO review — IRO-391)

The registry accepts packages only from supported package registries
(npm, PyPI, OCI, MCPB, NuGet, Cargo). IronClaw ships via Homebrew, `curl | sh`,
and the GHCR image — of these, **only the GHCR image is a registry-supported
package type**, so the listing uses the OCI form.

But `ironctl mcp serve` spawns each `sandbox_exec` run as a sibling gVisor
container, so a `docker run` launch of the image needs the **host Docker
socket** (`-v /var/run/docker.sock:/var/run/docker.sock` in
`server.json`'s `runtimeArguments`). Mounting the host Docker socket into a
container is effectively host-root and sits in tension with IronClaw's
hardening promise. Options:

- **A. OCI + docker socket (implemented here).** Works via plain `docker run`;
  documents the socket requirement explicitly. Trade-off: the socket exposure.
- **B. Dedicated slim MCP image** whose entrypoint is `ironctl mcp serve` and
  whose sandbox backend is scoped tighter than a raw socket mount. More work;
  cleaner trust story.
- **C. Native-only** (`command: ironctl, args: [mcp, serve]`, per
  `docs/mcp-server/`). This is the flow we already document and recommend, but
  it is **not a registry-supported package type**, so it cannot be the registry
  listing today.

This runbook ships the **A** implementation as the reviewable default; the
go/no-go on A-vs-B (and on listing at all under the socket trade-off) is the
CEO decision this issue is gated on.
