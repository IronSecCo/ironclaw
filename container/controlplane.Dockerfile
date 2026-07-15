# IronClaw control-plane image (the host daemon, published to GHCR).
#
# Ships the host daemon (/usr/local/bin/ironclaw-controlplane, from cmd/controlplane)
# and the admin CLI (/usr/local/bin/ironctl, from cmd/ironctl) on a minimal
# Debian-slim rootfs. This is the image docker-compose.yml runs and the one the
# `image.yml` workflow pushes to ghcr.io/<owner>/ironclaw-controlplane on release.
#
# This image is the HOST control-plane, not a sandbox. The sandboxes it launches are
# a SEPARATE image (container/Dockerfile, cmd/sandbox) run by the gVisor isolator
# with network=none — they are never started by docker-compose and never share this
# image's network. The control-plane itself DOES have egress (host-proxied model
# calls to api.anthropic.com); that asymmetry is the whole point and must not be
# flattened (see docker-compose.yml).
#
# The entrypoint mints the admin/API token on first run (printed once, no recovery)
# unless the operator supplies IRONCLAW_API_TOKEN. The image holds NO secrets: the
# model credential and any channel tokens arrive at runtime via the environment.
#
# Build from the repository root:
#   docker build -f container/controlplane.Dockerfile -t ironclaw-controlplane:latest .

# --- build stage ------------------------------------------------------------
FROM golang:1.23-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS build

# go-sqlcipher (the encrypted-queue binding) needs CGO + libcrypto headers; the
# control-plane links it transitively through the encrypted session queues.
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc libc6-dev libssl-dev \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src
# Prime the module cache first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Stamped into both binaries so `--version` reports the release tag; the image.yml
# workflow passes VERSION=<release tag>. Defaults to "dev" for a plain local build.
ARG VERSION=dev
ENV CGO_ENABLED=1
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/IronSecCo/ironclaw/internal/version.Version=${VERSION}" \
      -o /out/ironclaw-controlplane ./cmd/controlplane \
 && go build -trimpath \
      -ldflags "-s -w -X github.com/IronSecCo/ironclaw/internal/version.Version=${VERSION}" \
      -o /out/ironctl ./cmd/ironctl

# --- runtime stage ----------------------------------------------------------
FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818 AS runtime

# libssl3 satisfies the go-sqlcipher dynamic link; ca-certificates lets the
# host-proxied model egress validate TLS; curl backs the compose healthcheck
# against the unauthenticated /healthz endpoint. No other packages.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libssl3 ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

# Run the daemon as a non-root account (uid/gid 65532 mirrors distroless "nonroot").
# Launching real gVisor sandboxes needs a runsc-capable host and more privilege than
# the default compose grants; that is an advanced, documented step (deploy/README.md).
# The API + gateway the compose brings up need none of it.
RUN groupadd -g 65532 nonroot \
 && useradd -u 65532 -g 65532 -M -s /usr/sbin/nologin nonroot

COPY --from=build /out/ironclaw-controlplane /usr/local/bin/ironclaw-controlplane
COPY --from=build /out/ironctl /usr/local/bin/ironctl
COPY container/controlplane-entrypoint.sh /usr/local/bin/controlplane-entrypoint.sh

# Durable state (gateway store, audit log, sealed keys, the minted admin token) lives
# under /var/lib/ironclaw/state — compose mounts a named volume here. /run/ironclaw
# holds the model-proxy unix socket. Both are owned by the runtime uid so a fresh
# named volume initialises writable.
RUN mkdir -p /var/lib/ironclaw/state /run/ironclaw \
 && chown -R 65532:65532 /var/lib/ironclaw /run/ironclaw \
 && chmod 0700 /var/lib/ironclaw/state \
 && chmod +x /usr/local/bin/controlplane-entrypoint.sh

# OCI ownership proof for the official MCP Registry (registry.modelcontextprotocol.io).
# The registry's OCI validator fetches this image and requires this exact label to match
# the server.json `name`, proving the publisher controls the image before it will bind the
# `io.github.IronSecCo/ironclaw` listing to it. Without it, `mcp-publisher publish` fails
# closed. Keep this string in lock-step with server.json's `name` (see runbooks/mcp-registry.md).
LABEL io.modelcontextprotocol.server.name="io.github.IronSecCo/ironclaw"

USER 65532:65532
WORKDIR /var/lib/ironclaw
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/controlplane-entrypoint.sh"]
