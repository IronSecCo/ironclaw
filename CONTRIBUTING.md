# Contributing to IronClaw

> **By contributing to this repository, you agree to grant the project maintainers a
> permanent, non-exclusive, worldwide, royalty-free license to use, modify, and
> commercially dual-license your contributions.** This is formalized in the
> [Contributor License Agreement](CLA.md), which the CLA Assistant bot asks you to sign
> on your first pull request.

Thanks for your interest in IronClaw! Contributions of every kind are welcome —
bug reports, fixes, new channel adapters, docs, and tests.

## Ground rules

- **Open an issue first** for anything non-trivial, so we can agree on the approach
  before you invest time.
- **Keep changes small, focused, and reversible** — one concern per pull request.
- **No secrets** in code, tests, fixtures, or logs.
- Be excellent to each other — see the [Code of Conduct](CODE_OF_CONDUCT.md).

## Contributor License Agreement (CLA)

Before your first contribution can be merged, you'll sign our
[Contributor License Agreement](CLA.md). It confirms you have the right to contribute
your work and lets IronSecCo offer IronClaw under **both** the open-source AGPLv3 and a
commercial license (the project's [dual-license model](LICENSING.md)).

There's nothing to do up front: when you open your first pull request, the **CLA
Assistant** bot comments with a link, and you sign in one click with your GitHub
account. It remembers your signature for future PRs.

## Development setup

IronClaw is Go 1.23+ and requires **`CGO_ENABLED=1`** (the SQLCipher binding behind
the encrypted queues). Before opening a PR, make sure the standard checks pass:

```bash
export CGO_ENABLED=1
gofmt -l .      # must print nothing
go vet ./...
go build ./...
go test ./...
```

`make build vet test` runs the same checks. See [`docs/building.md`](docs/building.md)
for the full build notes.

## The frozen contract

`internal/contract/**` is the single seam shared by the control-plane (host) and the
sandbox — the **only** package both sides import. It is **frozen**: a drift here is a
silent decrypt failure or routing mismatch at runtime, not a build error. Every file
in it carries the banner
`// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md)`.

Changing it requires:

1. A joint RFC entry appended to [`docs/contract.md`](docs/contract.md).
2. Approval from the code owners (see [`CODEOWNERS`](CODEOWNERS)).

## Code layout

| Area | Path |
|------|------|
| Control-plane / host | `internal/host/**`, `cmd/controlplane`, `cmd/ironctl`, `api/`, `deploy/` |
| Sandbox runtime | `internal/sandbox/**`, `cmd/sandbox` |
| Shared frozen seam | `internal/contract/**` (see above) |
| Behavioral parity suite | `test/parity/**` (shared — add specs, don't rewrite others') |

## Pull requests

- Branch from `main`, make your change, and open a PR against `main`.
- Fill in the PR template, link the issue it closes, and make sure CI is green.
- A maintainer reviews and merges. We keep `main` releasable at all times.

## Good first contributions

Channel adapters (`internal/host/channels/`) are small, uniform, and dependency-free
— a great first PR. See [**Writing a channel adapter**](docs/writing-a-channel-adapter.md)
for the interface and house pattern, and [`docs/channels.md`](docs/channels.md) for how
each existing adapter is configured.
