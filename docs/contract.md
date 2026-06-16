# The frozen contract

`internal/contract` is the single seam shared by the control-plane (AGENT1) and
the sandbox (AGENT2). It is the **only** package both sides import, and it is
**frozen**.

## The freeze rule

Once the skeleton lands, neither agent may edit `internal/contract/**`
unilaterally. Every file there carries the banner:

```
// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).
```

A drift in this package is not a build error — it surfaces at runtime as a silent
decrypt failure (mismatched cipher params) or a routing mismatch (mismatched row
shapes). That is why the freeze is strict.

## The RFC process

To change the contract:

1. **Write an RFC.** Append a new dated section to the "RFC log" below describing
   the change, the motivation, and the migration impact on both trees.
2. **Get both CODEOWNERS to approve.** The control-plane owner and the sandbox
   owner must both sign off. This is enforced by `CODEOWNERS`, which lists
   `/internal/contract/` with both required reviewers.
3. **Land the contract change and both implementations together** so the host and
   sandbox never compile against divergent types or crypto params.

Pinned crypto parameters (`CipherScheme`, `CipherPageSize`, `KDFRawKey`) and seq
parity (host=even, sandbox=odd) are part of the contract and follow the same rule.

## RFC log

### RFC-0001 (proposed): add `OpenInboundRW`

**Status:** proposed, NOT applied. Requires both CODEOWNERS approvals per the
freeze rule.

**Motivation.** The host control-plane is the sole writer of the inbound queue:
the router enqueues platform messages (`messages_in`), upserts `destinations`, and
records `delivered` for dedup. To do that it must open the inbound DB
**read/write**. But `internal/contract/crypto.go` only exposes:

- `OpenInboundRO(path, k)` — sandbox-side, read-only inbound;
- `OpenOutboundRW(path, k)` — sandbox-side, read/write outbound;
- `OpenOutboundRO(path, k)` — host-side, read-only outbound.

There is **no** host-side read/write inbound opener. `internal/host/queue`
therefore cannot obtain a writable `*sql.DB` for inbound; its `openInboundRW`
helper currently returns a pending error that references this RFC, and the real,
parameterized SQL is written against `*sql.DB` so it activates the moment a real
handle is provided.

**Proposed change.** Add to `internal/contract/crypto.go`:

```go
// OpenInboundRW opens the inbound queue read/write (host side, sole inbound
// writer). It uses journal_mode=DELETE and the same raw-key discipline as the
// other openers, WITHOUT PRAGMA query_only (the host must write).
func OpenInboundRW(path string, k SessionKey) (*sql.DB, error)
```

It mirrors `OpenOutboundRW`'s connection string and PRAGMA ordering exactly,
minus `query_only`, so host and sandbox cannot drift on cipher params.

**Migration impact.**

- *Control-plane (AGENT1):* `internal/host/queue.openInboundRW` switches from the
  pending error to calling `contract.OpenInboundRW`; no other host change. The
  router/delivery write paths then activate.
- *Sandbox (AGENT2):* none. The sandbox never opens inbound read/write — the
  read-only-inbound type segregation is unchanged. This RFC does not weaken the
  type-level guarantee on the sandbox side.

**Why it is not applied here.** This control-plane pass is stdlib-only and may not
edit `internal/contract/**`. The change must land together with the
encrypted-SQLite binding and both CODEOWNERS' sign-off.
