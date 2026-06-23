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

A build that fails with a SQLite / cgo error almost always means the C toolchain or
`CGO_ENABLED` isn't set up — see
[Troubleshooting → Build fails with a SQLite / cgo error](troubleshooting.md#build-fails-with-a-sqlite-cgo-error).

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

See the [API reference](reference/api.md) for the HTTP API and
[../deploy/README.md](https://github.com/IronSecCo/ironclaw/blob/main/deploy/README.md) for the gVisor + Tailscale deployment.

## What's wired vs. what a live launch still needs

The control-plane is composed end-to-end. `cmd/controlplane` wires the durable
gateway + audit, key custody, the API + `/metrics`, the model proxy, the live
per-session lifecycle (a `SessionManager` over the encrypted-queue factory +
isolator), the maintenance sweep, the outbound delivery loop, and the channel
adapters — so `host/queue` opens real per-session SQLCipher databases and
`RouteInbound` / `Poll` / sweep run against them outside of `--dev`. The in-memory
backends remain only for `--dev` and tests.

The one piece that needs an external dependency at runtime is a **live sandbox
launch**: `isolation` builds the hardened OCI spec and provisions the bundle rootfs
through a pluggable provisioner (verifying the image digest/signature against a
trust policy), then execs the runtime — but a real `Launch` needs `runsc` and a
provisioned/signed image present in the host environment. You can build, vet, test,
and run the control-plane today; only that final exec needs gVisor + an image.

The remaining entrypoint task is attaching the API-server hardening knobs (optional
TLS, rate-limit, body limits, `/readyz` readiness gate): the `api.With*` options
exist but are not yet wired into `cmd/controlplane`. See the
[roadmap](roadmap.md) for the full status.
