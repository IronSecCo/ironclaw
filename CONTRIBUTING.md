# Contributing to IronClaw

> **By contributing to this repository, you agree to grant the project maintainers a
> permanent, non-exclusive, worldwide, royalty-free license to use, modify, and
> commercially dual-license your contributions.** This is formalized in the
> [Contributor License Agreement](CLA.md), which the CLA Assistant bot asks you to sign
> on your first pull request.

Thanks for your interest in IronClaw! Contributions of every kind are welcome —
bug reports, fixes, new channel adapters, docs, and tests.

New here? The [**documentation site**](https://ironsecco.github.io/ironclaw/) is the best
starting point — architecture, threat model, quickstart, channels, and skills in one place.

## Quickstart — your first PR in 5 minutes

From a clean checkout (Go 1.23+, with `CGO_ENABLED=1` for the SQLCipher binding):

```bash
# 1. Fork on GitHub, then clone your fork
git clone https://github.com/<you>/ironclaw && cd ironclaw

# 2. Build and run the checks (this is exactly what CI runs)
export CGO_ENABLED=1
make build vet test      # or: go build ./... && go vet ./... && go test ./...

# 3. Branch, make your change, and verify formatting
git checkout -b my-change
gofmt -l .               # must print nothing

# 4. Commit and push to your fork, then open a PR against `main`
git commit -am "docs: fix a typo"
git push -u origin my-change
```

Then open the pull request, fill in the template, and link the issue it closes.
The **CLA Assistant** bot comments on your first PR with a one-click signing link —
that's all the setup there is. A maintainer reviews and merges.

**Looking for something to work on?** Grab a
[**good first issue**](https://github.com/IronSecCo/ironclaw/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)
(comment to claim it first), or ask in
[**Discussions**](https://github.com/IronSecCo/ironclaw/discussions). The rest of this
guide covers the ground rules, the frozen contract, and the code layout in detail.

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

The fastest way in is a
[**good first issue**](https://github.com/IronSecCo/ironclaw/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)
— these are small, self-contained, and mentored. Comment on one to claim it before you start.

Channel adapters (`internal/host/channels/`) are also small, uniform, and dependency-free
— a great first PR. See [**Writing a channel adapter**](docs/writing-a-channel-adapter.md)
for the interface and house pattern, and [`docs/channels.md`](docs/channels.md) for how
each existing adapter is configured.

Have a question instead of a change? Open a thread in
[GitHub Discussions](https://github.com/IronSecCo/ironclaw/discussions).
