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

### RFC-0001 (applied): add `OpenInboundRW` + wire the encrypted-SQLite binding

**Status:** applied (owner sign-off). Adds `OpenInboundRW` and implements all four
`Open*` helpers over the SQLite3/SQLCipher cgo binding
(`github.com/mutecomm/go-sqlcipher/v4`). The whole tree now builds with `cgo`.

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

**As applied.** `internal/contract/crypto.go` now opens encrypted databases with
the per-session raw key carried in the DSN (`_pragma_key`), `journal_mode=DELETE`
(writers), `mode=ro` + `query_only` (readers), and `mmap_size=0` everywhere; the
cipher page size is left at SQLCipher v4's default (== the pinned `CipherPageSize`,
4096). `OpenInboundRW`/`OpenOutboundRW` also ensure their schema. A round-trip test
covers write→read, read-only write rejection, wrong-key failure, and absence of
plaintext on disk. `internal/host/queue.openInboundRW` now calls
`contract.OpenInboundRW`. The sandbox tree is unchanged except that the obsolete
"binding pending" test now asserts the live binding; the read-only-inbound
type-level guarantee is intact. `ErrCryptoBindingPending` is retained (no longer
returned) so the sandbox tree keeps compiling. CI builds with `CGO_ENABLED=1`.

### RFC-0002 (applied): pin the cross-seam wire formats (`actions.go`)

**Status:** applied. Adds `internal/contract/actions.go`; requires both CODEOWNERS'
sign-off per the freeze rule (control-plane owner + sandbox owner).

**Motivation.** Three things cross the host↔sandbox seam but were *not* in the
frozen contract — they lived informally in `internal/host/delivery` and
`internal/sandbox/tools`, with the host defining them and the sandbox
reverse-engineering them:

1. the **system-action envelope** the sandbox writes as a `KindSystem` outbound
   body (`{"action","payload","reason"}`), which host delivery parses and
   re-authorizes;
2. the **schedule_task request** body (`{"action","prompt","run_at","recurrence"}`)
   plus the named recurrence cadences (`hourly`/`daily`/`weekly`);
3. the **queue status vocabulary** (`queued`, `scheduled`, `processing`,
   `completed`, `delivered`) — the host writes inbound status + the delivered
   marker, the sandbox writes the outbound acks, and each side reads the other's.

Because none of these were pinned, a rename on either side compiled cleanly and
failed **silently at runtime** (a dropped system action, an unrecognized status) —
exactly the drift class the freeze rule exists to prevent. Operationally it forced
the sandbox to *wait and observe* the host's choices rather than build against a
spec, serializing what should be parallel work (the last two sandbox commits were
pure catch-up to host-defined formats; `internal/sandbox/queue` even carried a
`// Candidates to pin in the contract via RFC` note).

**Proposed change.** Add `internal/contract/actions.go` with, all `encoding/json`
+ `strings` only (no new dependency):

- `type SystemAction struct { Action string; Payload json.RawMessage; Reason string }`
  with `MarshalSystemAction`, `ParseSystemAction` (total — a bare body becomes an
  action name), and `SystemActionName`;
- `const ActionScheduleTask = "schedule_task"` (the one action name that is not a
  `ChangeKind`; capability actions use `string(ChangeKind)`);
- `type ScheduleRequest struct { Action, Prompt, RunAt, Recurrence string }` with
  `MarshalScheduleRequest` / `ParseScheduleRequest`, and
  `RecurrenceHourly/Daily/Weekly`;
- the `StatusQueued/Scheduled/Processing/Completed/Delivered` constants.

**What stays out of the contract (deliberately).** The *authorization policy* —
which actions are privileged and which `ChangeKind` they map to
(`delivery.authorizeSystemAction`) — remains host-internal. The sandbox must never
be able to define what counts as privileged; the contract pins only the wire
shapes, not the host's enforcement decision.

**Migration impact (landed together).**

- *Control-plane (AGENT1):* `delivery` uses `contract.SystemActionName`,
  `contract.ParseSystemAction`, `contract.ParseScheduleRequest`,
  `contract.ActionScheduleTask`, and `contract.StatusScheduled` (local
  `parseSystemAction` and `scheduleTaskPayload` removed); `scheduling` aliases its
  recurrence constants to the contract; `host/queue` writes
  `contract.StatusDelivered` / `StatusProcessing` / `StatusCompleted`.
- *Sandbox (AGENT2):* `tools.CapabilityChange.SystemActionJSON` marshals via
  `contract.SystemAction` (local `hostSystemAction` removed); `sandbox/queue`
  references the pinned status constants. No type-level guarantee changes.

The whole tree builds, vets, and tests green after the change.

## Capability-change payload conventions

These are cross-agent **conventions** layered on the frozen contract, not Go types
in `internal/contract`. They define the `payload` the sandbox puts in a
capability-change request and the `ChangeRequest.After` the host gateway
verifiers inspect. The wire path is:

1. Sandbox tool emits `{"action":"<kind>","payload":<obj>,"reason":"..."}` as the
   content of a `KindSystem` outbound message (`action` == the `ChangeKind`
   string, so it maps 1:1 to the host's `authorizeSystemAction`).
2. Host `delivery` routes it to a gateway `ChangeRequest` with that `Kind` and
   `After` = the `payload` verbatim (see `extractAfter`).
3. Gateway verifiers inspect `After`; then the mandatory human approves.

Per-kind `payload` shape:

| Kind | `payload` (== `After`) | Verifier |
|------|------------------------|----------|
| `packages` | `{"apt":["..."],"npm":["..."]}` (a flat `["..."]` is also accepted) | `PackageNameVerifier` rejects shell metacharacters |
| `mounts` | `[{"source":"/abs/host/path"}, ...]` | `MountAllowlistVerifier` rejects `..` and out-of-allowlist sources |
| `enabled_tools` | `["toolName", ...]` | none yet (human reviews) |
| `persona` | `{"instructions":"..."}` | none yet (human reviews) |
| `wiring` | object (engage mode, pattern, scope, ...) | none yet (human reviews) |
| `permissions` | object (role/member grants) | none yet (human reviews) |

Kinds with no automated verifier still pass through the deterministic chain and
the always-require-human floor; they are simply approved by a human without an
extra machine check. New verifiers may be added later and only ever ADD
rejections — never bypass the human floor.
