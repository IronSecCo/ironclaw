# Building

The skeleton builds, vets, and tests with the **Go standard library only** — no
external modules, no CGo. Set up Go and run:

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

## The one pending piece

The encrypted-queue binding is the single component not yet wired:
**SQLite3 Multiple Ciphers** (a SQLCipher-compatible scheme), which requires a
**CGo** build. Until it is added, the `contract.Open*` helpers return
`contract.ErrCryptoBindingPending`, and the skeleton compiles and tests cleanly
without it.

When the binding lands, the build will require `CGO_ENABLED=1` and a C toolchain.
The exact connection string and PRAGMA ordering are already centralized in
`internal/contract/crypto.go` (raw-key, `mode=ro`, `query_only`, `mmap_size=0`,
never `immutable=1`, DELETE journal, reopen-per-poll) so the host and sandbox
cannot drift.

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

## What stays pending in the control-plane

A few host paths are written but gated on the encrypted-DB seam: `host/queue`
(real SQL, no opener — see RFC-0001 in [contract.md](contract.md)), `host/router`
`RouteInbound`, `host/sweep` `Run`, and `host/delivery` `Poll`. Their pure logic
(`NamespaceUserID`, `EvaluateEngage`, `DecideStuckAction`, the gateway, keys,
model proxy, channels, and the API) is fully implemented and tested.
