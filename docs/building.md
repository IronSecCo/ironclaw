# Building

IronClaw builds, vets, and tests with **`CGO_ENABLED=1`** and a C toolchain — the
encrypted-SQLite binding is built via cgo (the SQLCipher C amalgamation is vendored
by the driver, so no system libsqlcipher is required). Set up Go + a C compiler and
run:

```sh
make build   # go build ./...
make vet     # go vet ./...
make test    # go test ./...
```

or directly:

```sh
go build ./...
go vet ./...
go test ./...
```

## Encrypted-queue binding (wired)

The per-session encrypted queues are live (**RFC-0001 applied**). `contract.Open*`
open SQLCipher databases via cgo (`github.com/mutecomm/go-sqlcipher/v4`): the raw
key travels in the DSN (`_pragma_key`) so every pooled connection is keyed before
any page read; readers use `mode=ro` + `query_only`, writers `journal_mode=DELETE`,
`mmap_size=0` everywhere, never `immutable=1`, reopen-per-poll. The connection
string and PRAGMA ordering are centralized in `internal/contract/crypto.go` so the
host and sandbox cannot drift. A round-trip test in `internal/contract` covers
write→read, read-only-write rejection, wrong-key failure, and no-plaintext-on-disk.
`internal/host/queue` uses the live binding; in-memory backends remain for `--dev`
and tests.

## Trees

- Host (control-plane) code: `internal/host/**`, `cmd/controlplane`,
  `cmd/ironctl`.
- Sandbox code: `internal/sandbox/**`, `cmd/sandbox`.
- Frozen shared seam: `internal/contract/**` (see [contract.md](contract.md)).

## Running the control-plane

The control-plane daemon and the admin CLI build with the standard library:

```sh
go build -o ironclaw-controlplane ./cmd/controlplane
go build -o ironctl              ./cmd/ironctl
```

Start the daemon. `--api-addr` should be the Tailscale (tailnet) IP in production
so the control-plane has no public port; `--model-proxy-socket` is the unix socket
the model proxy listens on (bound into each sandbox):

```sh
./ironclaw-controlplane --api-addr 127.0.0.1:8787 \
  --model-proxy-socket /run/ironclaw/modelproxy.sock
```

The gateway ships with the v1 floor verifier (`always-require-human`), so every
submitted change is held pending a human decision. Drive it with `ironctl`:

```sh
ironctl change submit --kind persona --group g1 --by slack:alice
ironctl change pending
ironctl change approve <id> --by slack:admin
ironctl change reject  <id> --by slack:admin
# point at a remote control-plane over the tailnet:
ironctl --addr http://<tailnet-ip>:8787 change pending
```

See [../api/control-plane.md](../api/control-plane.md) for the HTTP API and
[../deploy/README.md](../deploy/README.md) for the gVisor + Tailscale deployment.

## What stays gated

The encrypted binding is wired, so `host/queue` opens real per-session databases.
The remaining gated piece is **sandbox rootfs provisioning**: `isolation` builds
the hardened OCI spec and execs `runsc`, but unpacking an OCI image into the bundle
needs an external image tool, so `Launch` returns `ErrRootfsMissing` until a rootfs
is provisioned. Until the full per-session lifecycle (session dirs + key custody +
sandbox launch) is wired into the daemon, the control-plane runs with in-memory
backends under `--dev`; the per-session queue factories and live `RouteInbound` /
`Poll` / sweep wiring activate with that lifecycle. The pure logic
(`NamespaceUserID`, `EvaluateEngage`, `DecideStuckAction`, the gateway, keys,
model proxy, channels, queue SQL, and the API) is fully implemented and tested.
