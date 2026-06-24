# IronClaw sandbox image.
#
# Builds the in-sandbox agent entrypoint (/sandbox, from cmd/sandbox) and ships it
# on a minimal Debian-slim rootfs. The ContainerdProvisioner pulls + unpacks this
# image ONCE into a shared, digest-keyed rootfs; the runsc isolator then runs
# /sandbox under gVisor with network=none, all caps dropped, no_new_privs, and a
# read-only rootfs (see internal/host/isolation/oci.go).
#
# The image holds NO secrets and NO session key: the per-session key is delivered
# at launch via a tmpfs file, never baked into the image.
#
# Build from the repository root with container/build.sh (sets the build context).

# --- build stage ------------------------------------------------------------
FROM golang:1.23-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS build

# go-sqlcipher (the encrypted-queue binding) needs CGO + libcrypto headers.
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc libc6-dev libssl-dev \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src
# Prime the module cache first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ENV CGO_ENABLED=1
RUN go build -trimpath -ldflags "-s -w" -o /out/sandbox ./cmd/sandbox

# --- runtime stage ----------------------------------------------------------
FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df AS runtime

# libssl3 satisfies the go-sqlcipher dynamic link; ca-certificates is harmless and
# small. No other packages — the sandbox has network=none and installs nothing.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libssl3 ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# A non-root account; the isolator also runs the process as a non-zero uid inside a
# user namespace (OCI NonRootUID). uid/gid 65532 mirrors the distroless "nonroot".
RUN groupadd -g 65532 nonroot \
 && useradd -u 65532 -g 65532 -M -s /usr/sbin/nologin nonroot

COPY --from=build /out/sandbox /sandbox

# Mount points the OCI spec binds at launch (queues, model-proxy socket dir,
# workspace, and the optional durable /memory + read-only /shared). Pre-creating
# them keeps the read-only rootfs valid when the runtime mounts over them.
RUN mkdir -p /queue /run/ironclaw /workspace /memory /shared \
 && chown -R 65532:65532 /workspace \
 && chmod 0700 /workspace

USER 65532:65532
WORKDIR /workspace
ENTRYPOINT ["/sandbox"]
