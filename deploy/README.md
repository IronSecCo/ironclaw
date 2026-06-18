# Deploying the IronClaw control-plane

This directory holds the installer, service definitions, and notes for running the
control-plane host. The hardened runtime has three external dependencies that are
intentionally **not** vendored into the stdlib-only skeleton; install them on the
host before running in production.

## Quick start

```sh
sudo deploy/install.sh          # build + install binaries, config, and the service
```

[`install.sh`](install.sh) is a real, idempotent installer: it builds the
control-plane and `ironctl`, installs them under `/usr/local/bin`, provisions
`/etc/ironclaw/ironclaw.env` (0600; generates an API token on first run and
preserves it on re-install), and installs + enables the host service —
[`ironclaw.service`](ironclaw.service) (systemd/Linux) or
[`io.ironclaw.controlplane.plist`](io.ironclaw.controlplane.plist)
(launchd/macOS). Override defaults via the `IRONCLAW_*` env vars documented at the
top of the script.

## Components

1. **containerd + gVisor (`runsc`)** — every sandbox runs under gVisor via the
   `io.containerd.runsc.v1` runtime. gVisor interposes a user-space kernel between
   the sandbox and the host kernel, shrinking the syscall attack surface. The
   sandbox OCI spec sets `network=none`, drops all capabilities, sets
   `no_new_privs`, runs non-root in a user namespace, and uses a read-only rootfs
   with only a small writable `/workspace` (see `internal/host/isolation`).

   **Rootfs provisioning.** The pinned `/sandbox` image's filesystem is unpacked
   into each bundle's `rootfs/` by the `ContainerdProvisioner`
   (`internal/host/isolation/provisioner.go`), which shells out to containerd's
   `ctr` CLI — no extra image toolchain, no Go dependency added to the stdlib-only
   tree. The image is pulled and unpacked **once** host-side (the sandbox is
   `network=none`) into a shared, digest-keyed rootfs and reused read-only across
   sessions; `Launch` keeps a rootfs post-condition so a missing/broken
   provisioner fails loudly (`ErrRootfsMissing`) rather than starting an empty
   sandbox. Pre-pull the pinned image with
   `ctr -n ironclaw images pull <image>` (see [install.sh](install.sh)). Hosts that
   forbid `ctr images mount` (needs host `CAP_SYS_ADMIN`) can plug in an
   extract-based unpack or a bind/reflink materializer via the provisioner's
   options.

   **Durable per-group storage.** The rootfs is read-only; the sandbox's writable
   surface is mounts. Beyond the ephemeral tmpfs default, each sandbox can be given
   per-group **persistent** storage (see `SandboxSpec.WorkspacePath` /
   `MemoryPath` / `SharedReadOnlyPath` in `internal/host/isolation`):

   - `/workspace` — per-group durable scratch, bound **rw** (replaces the tmpfs).
   - `/memory` — per-group durable memory, bound **rw**.
   - `/shared` — a global, **read-only** shared-assets mount.

   All writable mounts carry `nosuid,nodev,noexec`; `/shared` is `ro`. Lay out the
   host dirs per agent group and chown them to the sandbox's mapped non-root uid
   (the distroless `nonroot`, uid `65532`); `/shared` is operator-managed and
   read-only. The isolator creates the rw dirs on launch if absent, but ownership
   is a deploy responsibility:

   ```sh
   install -d -m 0700 -o 65532 -g 65532 \
     /var/lib/ironclaw/groups/<group>/workspace \
     /var/lib/ironclaw/groups/<group>/memory
   install -d -m 0755 /var/lib/ironclaw/shared    # global, read-only to sandboxes
   ```

2. **Tailscale** — the control-plane API has **no public port**. Bind it to the
   host's tailnet IP and reach `ironctl` over the tailnet. A host firewall should
   drop inbound to the API port on every interface except the Tailscale one.

3. **The control-plane binary** — `cmd/controlplane`, run under
   [`ironclaw.service`](ironclaw.service) (systemd) or
   [`io.ironclaw.controlplane.plist`](io.ironclaw.controlplane.plist) (launchd).
   Both read all tunables from the 0600 `/etc/ironclaw/ironclaw.env`, so the unit
   files stay static and hold no secrets. The control-plane is the trusted host
   orchestrator (it launches gVisor sandboxes), so the unit is deliberately **not**
   self-confined — isolation is applied to the sandboxes, not to this process.

## Sandbox image

The `/sandbox` agent runs from a pinned container image built by
[`../container/build.sh`](../container/build.sh) from
[`../container/Dockerfile`](../container/Dockerfile) (build context is the repo
root). It compiles `./cmd/sandbox` with CGO (the SQLCipher binding) and ships it
on a minimal Debian-slim rootfs as a non-root user; it holds **no** secrets and
**no** session key. Pin the resulting digest in the provisioner trust policy
(`PinnedDigestPolicy`) and in `IRONCLAW_SANDBOX_IMAGE`:

```sh
container/build.sh ghcr.io/your-org/ironclaw-sandbox:1.0   # prints the RepoDigest to pin
```

## Running

The installer enables the service; to run the binary directly for development:

```sh
ironclaw-controlplane \
  --api-addr "$(tailscale ip -4):8787" \
  --model-proxy-socket /run/ironclaw/modelproxy.sock
```

Then, from a host on the tailnet:

```sh
ironctl --addr http://<tailnet-ip>:8787 change submit \
  --kind persona --group g1 --by slack:alice
ironctl --addr http://<tailnet-ip>:8787 change pending
ironctl --addr http://<tailnet-ip>:8787 change approve <id> --by slack:admin
```

## Model egress

The sandbox has no network. Its only outbound path is the host model proxy on the
bound unix socket, which enforces a destination allowlist (default:
`api.anthropic.com`) plus per-session/token rate caps, audit records, and response
secret redaction (`internal/host/modelproxy`).

See [install.sh](install.sh) for the installer and the `IRONCLAW_*` tunables it
honors.
