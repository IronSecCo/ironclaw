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
