# Contributing to IronClaw

IronClaw is built by two implementing agents working in parallel against a frozen
shared seam. Three rules keep them from colliding.

## 1. The frozen contract

`internal/contract/**` is the only package both sides import. It is **frozen**.
Neither agent may edit it unilaterally. Every file in it carries the banner:

```
// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).
```

Changing it requires:

1. A joint RFC entry appended to [`docs/contract.md`](docs/contract.md).
2. Approval from **both** CODEOWNERS (the control-plane owner and the sandbox
   owner). This is enforced by [`CODEOWNERS`](CODEOWNERS).

A drift in this package is a silent decrypt failure or a routing mismatch at
runtime — not a build error — so the freeze is strict.

## 2. Disjoint-tree ownership

Ownership is split so the two agents never edit the same files.

| Agent | Owns | Banner |
|-------|------|--------|
| AGENT1 (control-plane / host) | `internal/host/**`, `cmd/controlplane`, `cmd/ironctl`, `api/`, `deploy/`, `test/parity/harness` | `// OWNER: AGENT1` |
| AGENT2 (sandbox) | `internal/sandbox/**`, `cmd/sandbox` | `// OWNER: AGENT2` |

Every `.go` file in those trees starts with its owner banner. Neither agent edits
the other's tree.

## 3. Shared but additive

`test/parity/**` is shared: both agents may **add** specs. The `harness/`
sub-package is owned by AGENT1 (it boots the host) but exposes a documented
fake-sandbox hook that AGENT2's specs use. Do not rewrite each other's specs.

## Building

The skeleton builds, vets, and tests stdlib-only. See
[`docs/building.md`](docs/building.md). Run `make build vet test` before opening a
PR.
