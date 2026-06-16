<!-- OWNER: AGENT1 -->
# Deploying the IronClaw control-plane

This directory holds install notes and a scaffold script for running the
control-plane host. The hardened runtime has three external dependencies that are
intentionally **not** vendored into the stdlib-only skeleton; install them on the
host before running in production.

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

3. **The control-plane binary** — `cmd/controlplane`, typically run under a
   systemd unit (or launchd on macOS).

## Running

```sh
go build -o /usr/local/bin/ironclaw-controlplane ./cmd/controlplane
go build -o /usr/local/bin/ironctl ./cmd/ironctl

# Bind the API to the tailnet IP; serve the model proxy on a unix socket that the
# isolator will bind into each sandbox.
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
`api.anthropic.com`). Per-token caps, logging, and redaction are future work.

See [install.sh](install.sh) for a commented, step-by-step scaffold.
