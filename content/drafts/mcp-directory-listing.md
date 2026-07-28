# MCP directory listing copy (parked)

Status: **parked, not posted.** Preserved from IRO-378, which the board cancelled on
2026-07-29 (decision on IRO-606, interaction `96a6b54d`, answer `g3_cancel`).

Third-party MCP directories that need a registered account behind a web form
(Smithery, mcp.so, mcpservers.org) were declined by the board. The account-free PR
paths are exhausted: `glama-ai/mcp-registry` returns 404 and the
`modelcontextprotocol/servers` third-party list is deprecated in favour of the
official registry.

This file exists so the copy is not lost with the cancelled issue thread. Every
claim below is mapped to shipped behaviour (PR #357, `docs/integrations/mcp-server.md`,
`internal/host/sandboxexec`) so that whoever revives a listing asserts nothing
IronClaw does not do.

## Registry state at time of parking (2026-07-29)

The official MCP Registry listing is **not currently live**, contrary to the common
assumption that OIDC auto-publishes on every release:

- All 10 published versions (`v0.1.216`–`v0.1.230`) were deliberately set
  `deprecated` on 2026-07-07 by `.github/workflows/mcp-registry-deprecate.yml`,
  because they launched IronClaw with a host Docker-socket mount.
- The `publish-mcp-registry` job in `.github/workflows/image.yml` is parked behind
  `workflow_dispatch` + input `publish_mcp_registry` (default false), so the release
  path no longer publishes.
- `server.json` still points at
  `ghcr.io/ironsecco/ironclaw-controlplane` with the `/var/run/docker.sock` mount.
- The socket-free replacement image `ghcr.io/ironsecco/ironclaw-mcp` is defined by
  `container/mcp.Dockerfile` (IRO-414, merged) but is **not built or pushed by any
  workflow**, and is not present on GHCR.
- `container/mcp.Dockerfile` carries the required
  `io.modelcontextprotocol.server.name` label only as a commented placeholder; the
  registry's OCI validator fails closed without it.

Relighting the listing is account-free (GitHub Actions OIDC) and is tracked
separately as engineering work; it is not part of the declined account-gated scope.

## Canonical listing metadata

- **Name:** IronClaw
- **Namespace (official registry):** `io.github.IronSecCo/ironclaw`
- **Tagline:** Sandbox any MCP tool call inside gVisor.
- **Short (<=160 chars):** MCP server exposing one tool, `sandbox_exec`, that runs
  untrusted model-generated commands inside an ephemeral gVisor (runsc) box: no
  network, all caps dropped, non-root, read-only rootfs, seccomp.
- **Repo:** https://github.com/IronSecCo/ironclaw
- **License:** AGPL-3.0 + Commercial
- **Category:** security / sandboxing / code-execution
- **Transport:** stdio (default); streamable-HTTP (loopback default; a routable
  address is refused without `IRONCLAW_MCP_AUTH_TOKEN`)

Note: the registry schema caps `description` at 100 characters, which is shorter
than the 160-char blurb above. The shipped `server.json` uses:
"Sandboxed shell exec for MCP clients: run untrusted agent commands in a gVisor container."

## Client config (stdio)

```json
{
  "mcpServers": {
    "ironclaw": { "command": "ironctl", "args": ["mcp", "serve"] }
  }
}
```

## The one tool: `sandbox_exec`

| Arg | Type | Default | Meaning |
|---|---|---|---|
| `command` | string (required) | | Command run in the box via `sh -c`. |
| `image` | string | `docker.io/library/alpine:3.20` | Image override; rejected if it starts with `-`. |
| `timeout_seconds` | integer | `30` | Per-exec timeout; the schema bounds it to 1-600. |

Returns stdout, stderr, exit code, and a `containment:` line naming the actual
runtime. Every call is a `docker run --rm` on an ephemeral `ic-sbx-mcp-*` box:
`--runtime runsc` (gVisor; a non-runsc runtime is a labelled, fail-closed
fallback), `--network none`, `--cap-drop ALL`, restrictive seccomp,
`--security-opt no-new-privileges`, `--read-only` rootfs plus tmpfs `/tmp`,
`--user 65532:65532`, `--pids-limit 256 --memory 512m --cpus 1`.

## Packaging note

IronClaw's MCP server is the `ironctl` Go binary invoked as `ironctl mcp serve`,
distributed via Homebrew, `curl | sh`, and GitHub releases. Official MCP Registry
package types are `npm | pypi | nuget | oci | mcpb`, none of which map to a
brew/binary install. The chosen path is a dedicated OCI image
(`ghcr.io/ironsecco/ironclaw-mcp`, `container/mcp.Dockerfile`) whose entrypoint runs
the MCP server with no host Docker socket. See the registry-state section above for
what still has to happen before that image can back a listing.

## Positioning angle

Lead every listing with containment:

> Most "run code" MCP servers execute model output **on your machine**. IronClaw's
> `sandbox_exec` runs it inside an ephemeral gVisor box with no network and a
> read-only rootfs, so a prompt injection or a hallucinated command cannot reach
> your host.
