# IronClaw slim MCP-server image  ->  ghcr.io/ironsecco/ironclaw-mcp
#
# A privilege-free MCP server for the official MCP Registry. It ships ONLY the
# static `ironctl` binary and runs `ironctl mcp serve` in THIN-CLIENT mode: the
# sandbox_exec tool delegates every box to a running IronClaw control-plane over its
# authenticated API (POST /v1/sandbox/exec). The control-plane owns the hardened
# gVisor (runsc) spawning; THIS container holds NO host privilege:
#
#   * NO /var/run/docker.sock mount (no host Docker control)
#   * NO runsc / OCI runtime in the image
#   * static binary, non-root, read-only-friendly rootfs, no shell
#
# That is the trade-off CEO chose (IRO-414, option B) over listing the full
# control-plane image with a host Docker-socket mount. Fail-closed: with no
# control-plane reachable the tool errors; it never falls back to host docker.
#
# Runtime configuration (env, set by the MCP client / registry server.json):
#   IRONCLAW_CONTROLPLANE_URL   required — base URL of the control-plane (e.g. http://cp:8787)
#   IRONCLAW_API_TOKEN          control-plane API bearer token (name overridable via --token-env)
#
# Build from the repository root:
#   docker build -f container/mcp.Dockerfile -t ghcr.io/ironsecco/ironclaw-mcp:latest .

# --- build stage ------------------------------------------------------------
FROM golang:1.23-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS build

WORKDIR /src
# Prime the module cache first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# ironctl is a pure HTTP/stdio client: it does NOT link the encrypted-queue
# (go-sqlcipher) binding, so it builds fully static with CGO disabled. A static
# binary lets us ship on distroless/static with no libc, no shell, no package
# manager — the minimal attack surface the registry image needs.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w" -o /out/ironctl ./cmd/ironctl

# --- runtime stage ----------------------------------------------------------
# distroless static: no shell, no package manager, ships ca-certificates and a
# nonroot (65532) user. Nothing else is needed — the box lifecycle lives on the
# control-plane, not here.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# NOTE (handoff to IRO-391 / Relay): add the MCP Registry ownership label here, e.g.
#   LABEL io.modelcontextprotocol.server.name="<name from server.json>"
# It is intentionally omitted so the exact name stays owned by server.json, which
# Relay maintains alongside the publish-mcp-registry job.

COPY --from=build /out/ironctl /ironctl

USER 65532:65532
# stdio MCP: the client spawns this container with -i and speaks newline-delimited
# JSON-RPC over stdin/stdout. --controlplane defaults to $IRONCLAW_CONTROLPLANE_URL,
# so thin-client mode engages automatically when that env is set; there is no docker
# in this image, so it can never run a box locally.
ENTRYPOINT ["/ironctl", "mcp", "serve"]
