---
title: "The frozen contract: the control-plane and sandbox seam"
description: internal/contract is the single frozen seam shared by the IronClaw control-plane and the sandbox. Learn the freeze rule, the RFC process, and why a stable wire boundary keeps isolation trustworthy.
---

# The frozen contract

`internal/contract` is the single seam shared by the control-plane and
the sandbox. It is the **only** package both sides import, and it is
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

## Seq allocation invariant (concurrency)

Both message streams carry a `seq INTEGER UNIQUE` column. The single load-bearing
rule for allocating it:

> **Mint the next seq atomically inside the INSERT — never with a separate
> read-modify-write, and never from more than one uncoordinated minter per
> stream.**

IRO-278 was a violation of this rule: three uncoordinated inbound minters (the
router used an in-memory counter, while the sweep and delivery re-enqueuers each
did `SELECT MAX(seq)+2` in Go) raced and picked the same seq, so one INSERT failed
on `UNIQUE(seq)`. That broke recurring `schedule_task` and intermittently 500'd
`/chat/send`. IRO-283 audited every seq/sequence write path and generalized the
fix so the class cannot recur:

| Stream / site | Table | Allocation | Status |
|---|---|---|---|
| `hostInbound.WriteMessageIn` (router, sweep, delivery, a2a all pass `Seq==0`) | `messages_in` (even) | `COALESCE((SELECT MAX(seq) FROM messages_in), 0) + 2` inside the INSERT | authoritative (IRO-278) |
| `sandboxOutbound.WriteMessageOut` (sandbox, sole writer) | `messages_out` (odd) | `(COALESCE((SELECT MAX(seq) FROM messages_out), -1) + 1) \| 1` inside the INSERT | authoritative (IRO-283); no longer depends on the `s.mu` lock or on staying single-writer |
| `questions.Store.Record` (`s.seq++`) | in-memory map, not persisted, no `UNIQUE` | mutex-guarded counter for question IDs only | benign — not a message-stream seq |

Callers on the inbound path MUST pass `Seq==0` and let the writer mint; the
outbound writer ignores any caller-supplied `Seq` outright. The odd expression
forces the result odd (`| 1`) so parity holds even if a stray even seq ever
appeared, and is provably equivalent to the `nextOddSeq` reference spec.

Regression coverage is deterministic and runs under `-race` in CI (the `race` job
in `ci.yml`): `TestHostInboundConcurrentWritersNoCollision` fans out independent
inbound writer handles (router + sweep + delivery contention) and
`TestSandboxOutboundConcurrentWritersNoCollision` fans out two independent outbound
handles; both assert N distinct, correctly-parified seqs with zero collisions. A
reintroduced uncoordinated minter fails that job instead of shipping.

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

- *Control-plane:* `internal/host/queue.openInboundRW` switches from the
  pending error to calling `contract.OpenInboundRW`; no other host change. The
  router/delivery write paths then activate.
- *Sandbox:* none. The sandbox never opens inbound read/write — the
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

- *Control-plane:* `delivery` uses `contract.SystemActionName`,
  `contract.ParseSystemAction`, `contract.ParseScheduleRequest`,
  `contract.ActionScheduleTask`, and `contract.StatusScheduled` (local
  `parseSystemAction` and `scheduleTaskPayload` removed); `scheduling` aliases its
  recurrence constants to the contract; `host/queue` writes
  `contract.StatusDelivered` / `StatusProcessing` / `StatusCompleted`.
- *Sandbox:* `tools.CapabilityChange.SystemActionJSON` marshals via
  `contract.SystemAction` (local `hostSystemAction` removed); `sandbox/queue`
  references the pinned status constants. No type-level guarantee changes.

The whole tree builds, vets, and tests green after the change.

### RFC-0003 (applied): add the `ask_user_question` non-privileged system action

**Status:** applied (human-authorized this session; landed with both tree
implementations together per the RFC process). Adds a second non-`ChangeKind`
system action alongside `schedule_task`.

**Motivation.** An agent often needs a human decision mid-task ("which environment
should I deploy to?") with a small set of choices — a *choice card*. There was no
wire shape for this. Without one, a sandbox emitting an unrecognized action hits
`delivery.authorizeSystemAction`'s default case, which conservatively treats
unknown actions as **privileged** and routes them through the gateway — the wrong
behavior for a question, which mutates nothing and needs no approval. So the action
must be pinned in the contract (like `schedule_task`) and recognized host-side as
non-privileged.

**Proposed change.** Add to `internal/contract/actions.go`:

```go
const ActionAskUser = "ask_user_question" // non-privileged, NOT a ChangeKind

type AskUserRequest struct {
    Action        string   `json:"action"`
    Question      string   `json:"question"`
    Options       []string `json:"options,omitempty"`
    AllowFreeform bool     `json:"allow_freeform,omitempty"`
}
func MarshalAskUserRequest(AskUserRequest) (string, error) // forces Action
func ParseAskUserRequest(string) (AskUserRequest, error)
```

It mirrors `ScheduleRequest`: a flat shape sharing only `action` with the
`SystemAction` envelope, carrying ONLY a question + preset choices — **no
script/command field and no capability mutation** — so it can never become an
execution or escalation path. It is additive and backward-compatible (no existing
shape changes).

**Migration impact.**

- *Control-plane:* `delivery.handleSystem` special-cases `ActionAskUser` before the
  privilege routing (exactly as it already does for `schedule_task`) and records the
  question in a new in-memory `internal/host/questions` pending-question store;
  `authorizeSystemAction` gains an explicit non-privileged case for it. Feeding the
  human's answer back to the session as inbound is a follow-on; this RFC covers the
  wire shape + host-side tracking.
- *Sandbox:* a new `tools.AskUserQuestionTool` (a `HostForwarder`, like
  `ScheduleTaskTool`) emits an `AskUserRequest`; it performs no privileged action.
- Both land together, so neither tree compiles against a half-defined action.

The whole tree builds, vets, and tests green after the change.

### RFC-0004 (proposed): agent-to-agent messaging + approval-gated `create_agent`

**Status:** APPROVED & LANDED (2026-06-16) — signed off by the sole CODEOWNER
(`@omerzamir` / maintainer) via the decision to un-gate the a2a + create_agent change. Implemented
(`#20`): the single contract edit (`ChangeCreateAgent`) plus the host-internal
`create_agent` verifier/applier (`internal/host/gateway/create_agent.go`), the
sandbox `create_agent` tool, and a2a routing with hop-depth + send-quota safety
(`internal/host/delivery/a2a.go`). The maintainer's answers to the open questions
below are recorded inline. Daemon composition (chaining the verifier/applier,
wiring agent-destination grants, restricting create_agent approval to owners/admins
via the PolicyApprover) is wired by the daemon.

**Motivation.** Two capabilities are missing: (1) an agent has no way to hand work
to *another* agent group (agent-to-agent, "a2a"); (2) there is no way to create a
new agent group at runtime under human control. Both are privileged, mutating
operations, so they must respect the same trust boundary as every other
control-plane mutation — never an unapproved escalation path.

**Design principle — minimize the contract surface.** Only what crosses the
host↔sandbox seam belongs in `internal/contract`. Under that lens:

- **`create_agent` needs ONE new contract value:** a new `ChangeKind`. It then
  rides the *existing* `SystemAction` envelope and gateway machinery — no new wire.
- **a2a needs ZERO contract change.** The sandbox already addresses outbound
  targets by *name* via the `send_message` tool and the `Destination`
  rows; the host resolves a name to either a platform channel or an agent group.
  Routing a message to an agent group is therefore a host-internal decision. The
  `Destination` row already carries `Type` and `AgentGroupID` fields for exactly
  this.

**Proposed contract change (the only frozen-file edit).** Add to
`internal/contract/enums.go`:

```go
// ChangeCreateAgent provisions a NEW agent group. Privileged: always routed
// through the gateway's mandatory human-approval floor (a new agent is a new
// trust principal). The payload describes the proposed agent group; see the
// payload-conventions table.
ChangeCreateAgent ChangeKind = "create_agent"
```

`create_agent` payload convention (host-internal, == `ChangeRequest.After`, layered
on the existing capability-change wire — `action == "create_agent"`):

```json
{
  "name": "string",                         // required; human-readable
  "folder": "string",                       // optional; derived from name if absent
  "persona": {"instructions": "..."},       // optional initial persona
  "enabled_tools": ["..."],                 // optional
  "members": ["slack:alice", ...],          // optional initial access grants
  "wirings": [ { /* engage/session/scope */ } ]  // optional initial wirings
}
```

**Host-internal design (NOT contract).**

- *create_agent applier + verifier.* `delivery.authorizeSystemAction` maps
  `create_agent` to `ChangeCreateAgent` (privileged → gateway). A new
  `CreateAgentVerifier` validates: `name`/`folder` carry no path traversal (`..`)
  or shell metacharacters; the derived `AgentGroupID` does not already exist;
  initial `members`/`wirings` are well-formed. The mandatory human floor always
  applies (a new principal is never auto-approved). On approval the applier calls
  `registry.PutAgentGroup` (+ optional initial wirings/members) — all existing
  Registry methods.
- *No privilege inheritance.* The creating agent may only grant the new agent
  access it could already grant (scope check against the creator's roles); a new
  agent starts with the **minimum** capability set, never the creator's.
- *a2a routing.* `send_message` to a destination whose `Type == "agent"` (with
  `AgentGroupID` set) is routed by `delivery` **inbound** to the target group via
  the existing router/session resolution, instead of to a platform adapter.
  Authorization reuses destination allow-listing: an agent may message only the
  agent groups it is explicitly permitted to (a new `registry` agent-destination
  check). Provenance is stamped via the existing `MessageIn.SourceSessionID`.
- *Loop / amplification safety.* a2a carries a bounded hop depth (derived from
  `SourceSessionID` provenance) so messages cannot ping-pong indefinitely; per-agent
  send quotas bound fan-out. `create_agent` is rate-limited (pending-request cap) to
  prevent agent-bombing.

**Migration impact (must land together per the freeze rule).**

- *Contract:* add `ChangeCreateAgent` (additive; no existing shape changes).
- *Control-plane:* `CreateAgentVerifier` + applier; `delivery` agent-destination
  routing + `authorizeSystemAction` case; `registry` agent-destination storage +
  access check.
- *Sandbox:* a `create_agent` tool (a `HostForwarder`, like the capability tools);
  a2a reuses the existing `send_message` tool unchanged.

**Maintainer decisions (resolved 2026-06-16):**

1. **Who may create agents** — **owners + admins.** Every create_agent still hits
   the human floor; restricting the *approver* to owner/admin roles is done with the
   `PolicyApprover` (`ApproverRoles{create_agent: [owner, admin]}`), wired by
   the daemon. The `CreateAgentVerifier` hard-requires a human regardless, so
   create_agent can never be auto-approved even if misconfigured into a policy.
2. **a2a posture** — **allow within a trust group**, expressed as deny-by-default
   agent-destination grants the operator configures among trust-group members. The
   mechanism reuses the existing registry destination allowlist with the `"agent"`
   channel sentinel (`IsAllowedDestination(sender, "agent", targetGroupID)`).
3. **a2a hop-depth limit = 5; per-agent send quota = 120/min.** Both are
   configurable via `delivery.Delivery.WithA2ALimits`.
4. **Kept host-internal.** The `"agent"` sentinel / destination type is NOT pinned
   in the contract; the host resolves type. The contract stays minimal — the only
   frozen-file edit is `ChangeCreateAgent`.
5. **Landed together.**

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
| `mcp_access` | `{"server":"<name>","tools":["<tool>",...]}` (omit `tools` = all the server's declared tools) | `MCPServerVerifier` rejects an unknown server / a tool the server does not declare |
| `skill_install` | `{"skill":"<name>","version":"<version>"}` | host RESOLVES the named skill (curated source + minisign trust root); an unknown/unsigned/out-of-policy skill is refused before the gateway |
| `mcp_register` | `{"name","transport":"stdio"\|"http", stdio: "command","args","image","env" \| http: "url","headers"}` | `MCPRegisterVerifier` rejects when MCP is disabled / a malformed def (reusing the catalog's `ServerConfig.Validate`); else require-human |

### RFC-0005: approval-gated MCP-server access — `ChangeMCPAccess`

MCP (Model Context Protocol) servers extend an agent with externally-served tools.
The reference design wired them with a **blind approval surface** — the one gap
IronClaw exists to close — so IronClaw runs every MCP server **host-side** and gates
access through the gateway:

- **One new contract value:** `ChangeMCPAccess = "mcp_access"` (this file's only
  MCP-access edit). It rides the existing `SystemAction` envelope (`action ==
  "mcp_access"`); `delivery.authorizeSystemAction` maps it (privileged → gateway).
- **The human approves a NAMED server and NAMED tools** — never "whatever the
  server exposes". The `After` payload names them; the approved grant is recorded on
  the agent group (`registry.GrantedMCP`) and materialized at launch.
- **MCP servers never run in the sandbox.** A *local* server is a stdio subprocess
  on the host; a *remote* server is an HTTPS endpoint the host dials. Either way a
  per-session **broker unix socket** (`/run/ironclaw/mcp.sock`, like the model-proxy
  and egress sockets) exposes only the approved tool surface to the sandbox, which
  stays `network=none` and never speaks MCP itself. Credentials are injected
  host-side (`${ENV}` references resolved by the broker) and never reach the sandbox.
- **Kept host-internal.** The MCP server catalog, the `GrantedMCP` shape, and the
  broker wire format are host-internal — not pinned in the contract.

### RFC-0006: in-session skill install — `ChangeSkillInstall`

OpenClaw's headline loop is *tell the assistant in chat to add a skill → a human
approves → it executes in the same session.* IronClaw already supported this shape for
`mcp_access`; RFC-0006 closes the gap for **skills**, which were previously
operator-only (out-of-session `ironctl skill add` / `POST /v1/skills/install`).

- **One new contract value:** `ChangeSkillInstall = "skill_install"`. It rides the
  existing `SystemAction` envelope (`action == "skill_install"`); the agent reaches it
  from chat via `request_capability_change` (kind `skill_install`).
- **The sandbox may only NAME a skill** (`{"skill","version"}`) — it can never author
  skill content (persona text, tool grants, asset bundles). That invariant is the whole
  reason skills require operator-curated, minisign-signed bundles.
- **The host resolves through the SAME trust gate as the operator path.**
  `delivery.handleSkillInstall` calls `skills.InstallChange` over the configured
  `Resolver` (curated source + trust root): fetch + signature-verify + manifest-validate.
  The resolved install is submitted as a **`ChangePermissions`** ChangeRequest — the
  `skill_install` kind is the sandbox→host action vocabulary only and is **never itself a
  `ChangeRequest.Kind`** — so the proven skill-install applier + respawn handle it exactly
  as the operator path, and the human approves the real persona/tools/egress it grants.
- **Fail-closed.** With skills not enabled (no resolver), or a skill that is
  unknown/unsigned/out-of-policy, the proposal is refused host-side and never reaches the
  gateway. The sandbox can *ask*; only a curated+signed skill an operator provisioned can
  be proposed, and a human still approves it.

### RFC-0007: in-session MCP-server registration — `ChangeMCPRegister`

`mcp_access` (RFC-0005) lets an agent ask for tools on a server **an operator already
configured**. OpenClaw's full loop is *register → approve → access → execute*: the
assistant can propose a brand-new server endpoint in chat, a human approves it, and only
then is it usable. RFC-0007 closes that first hop — **without** weakening the blind-MCP
gate RFC-0005 closed.

- **One new contract value:** `ChangeMCPRegister = "mcp_register"`. It rides the existing
  `SystemAction` envelope (`action == "mcp_register"`); the agent reaches it from chat via
  `request_capability_change` (kind `mcp_register`). `delivery.authorizeSystemAction` maps
  it (privileged → gateway).
- **The agent only PROPOSES the endpoint.** The `After` payload is an `mcp.ServerConfig`
  definition (name + transport + the stdio `command`/`args`/`image` or http `url`/
  `headers`). The `MCPRegisterVerifier` is **deny-by-default** (reject when MCP is
  disabled), rejects a malformed def by reusing the catalog's own `ServerConfig.Validate`
  (empty name, not exactly one of command/url, non-`https` url to a non-loopback host,
  unknown fields), and otherwise returns **require-human** — never auto-approved.
- **The human approves the EXACT command/url.** Registering a server introduces a new
  code-execution / egress surface, so the approval card surfaces the full endpoint
  definition (secrets in `env`/`headers` masked via `ServerConfig.Public`). On approval the
  `MCPRegisterApplier` calls `catalog.Put` + `broker.Invalidate(name)`.
- **Registration grants the proposing agent NOTHING.** An approved register lands the
  server in the catalog only; the agent must still obtain its tools through the separate,
  also-human-gated `mcp_access` approval (least-privilege — registering an endpoint and
  being allowed to call it are two independent human decisions). The server still runs
  **host-side** behind the per-session broker socket, so the sandbox stays `network=none`
  and never speaks MCP itself.

Kinds with no automated verifier still pass through the deterministic chain and
the always-require-human floor; they are simply approved by a human without an
extra machine check. New verifiers may be added later and only ever ADD
rejections — never bypass the human floor.
