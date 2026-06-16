# IronClaw — Skeleton & Two-Agent Build Plan

## Context

IronClaw is a new, security-hardened, open-source assistant platform written entirely in **Go**. It reimplements the behavior of an existing TypeScript/Bun system (referred to here only as "the reference"; **its name must never appear in the IronClaw repo**), but redesigns the trust model from the ground up.

**Why this is being built.** A security review of the reference found that its "secure by isolation" claim was not backed by a hardened trust boundary: the host trusted data the sandbox wrote; an agent could edit its own runtime; prompt-injection could reach privileged actions (notably an unapproved `script` field giving RCE); self-modification and MCP wiring had blind approval surfaces; and queues were plaintext on disk. IronClaw fixes the *class* of these problems by design:

1. **Compiled Go, no interpreter in the sandbox** → the agent cannot read or edit its own source.
2. **A mandatory gateway** → every control-plane mutation is a deterministic, human-approved, auditable transaction with a pluggable (future) automated-verification step. No file is the source of truth for agent config.
3. **Encrypted queues** (per-session key) → disk theft and cross-session reads are useless.
4. **Least-privilege queue access** → the sandbox can only read inbound and write outbound, enforced at the Go type level *and* the OS mount level.
5. **gVisor** wraps every sandbox (pluggable Isolator; Kata/Firecracker noted for later).
6. **Tailscale** fronts the control-plane API (no public port).
7. **Sandbox has no network**; model calls go through a host proxy.

The deliverable of *this* plan is the **compiling skeleton** of the full project: the frozen shared contract, stub packages for both implementing agents with clear ownership markers, OSS scaffolding, and a parity-test harness — so two agents can then implement in parallel without colliding.

## Locked decisions

| Area | Decision |
|------|----------|
| Language | Pure Go, end to end. License **MIT**. |
| Agent loop | Reimplemented in Go; `Provider` interface, first impl = Anthropic Messages API (tool use + streaming). |
| Isolation | gVisor (`runsc`) via **containerd Go client** + `io.containerd.runsc.v1`. Pluggable `Isolator`; Kata (future Firecracker) documented, not built. |
| Queues | Per-session pair of **encrypted SQLite** DBs (inbound + outbound). **SQLite3 Multiple Ciphers** (SQLCipher-compatible scheme) via a CGo build; **raw-key mode** (no per-open KDF). Per-session 256-bit key shared host↔that one sandbox. |
| Queue access | Sandbox: **read-only** inbound, **read/write** outbound. Enforced by interface segregation (no write method exists) + `PRAGMA query_only` + OS `ro` bind mount. |
| Gateway | **All control-plane mutations** flow through it: persona/instructions, enabled tools, packages, routing/wiring, permissions, mounts. Deterministic verifier chain → human approval → idempotent apply. v1 floor = always-require-human. |
| Mesh | Tailscale secures **remote admin/control-plane access only**. Agent egress firewalling documented as a later extension. |
| Model egress | **Host-proxied; sandbox `network=none`.** Sandbox reaches a host model-proxy over a single bound unix socket; host enforces allowlist, can cap/log/redact. |
| Naming | Plain descriptive names (no metaphors): control-plane, sandbox, persona, session, queue, gateway, key, isolation, mesh. |
| Build split | Agent 1 = control-plane (host). Agent 2 = sandbox. Frozen `internal/contract` seam between them. |

## Target repo

Repository: `github.com/nivardsec/ironclaw`. `go.mod` module path `github.com/nivardsec/ironclaw`.

## Repository layout

```
ironclaw/
  LICENSE                      # MIT
  README.md                    # what it is, threat model, quickstart
  CONTRIBUTING.md              # contract-freeze rule, agent-ownership rule
  CODEOWNERS                   # internal/contract requires both agents' review
  Makefile                     # build, test, lint, parity
  go.mod  go.work              # go.work joins host+sandbox build tags
  .github/workflows/ci.yml     # build (CGo), vet, test, parity
  cmd/
    controlplane/main.go       # AGENT 1 — host daemon entrypoint
    sandbox/main.go            # AGENT 2 — in-sandbox agent entrypoint
    ironctl/main.go            # AGENT 1 — admin CLI (talks to control-plane API)
  internal/
    contract/                  # FROZEN SEAM — authored in skeleton, see below
      schema.go  rows.go  ids.go  enums.go
      crypto.go  queue.go  gateway.go  doc.go
    host/                      # AGENT 1 ONLY
      api/        gateway/     isolation/   router/
      delivery/   sweep/       keys/        channels/
      modelproxy/
    sandbox/                   # AGENT 2 ONLY
      loop/       provider/    tools/       queue/
  api/                         # proto/OpenAPI for control-plane + gateway (AGENT 1)
  deploy/                      # install scripts: gVisor, containerd, tailscale, systemd (AGENT 1)
  docs/
    architecture.md  threat-model.md  contract.md  building.md
  test/
    parity/                    # black-box behavioral suite (shared; see rules)
      harness/  routing_test.go  engage_test.go  session_test.go
      delivery_test.go  gateway_test.go  crossmount_test.go
```

## The frozen contract (`internal/contract`) — authored in the skeleton

This package is the only code both agents import. **Neither agent may edit it** after the skeleton lands; changes require a joint RFC note in `docs/contract.md` and both CODEOWNERS approvals. It must compile and be complete enough that both sides can build against stable types.

Contents:

- **`ids.go`** — typed IDs to prevent mixups: `SessionID`, `MessageID`, `AgentGroupID`, `MessagingGroupID`, `UserID`, `ChangeID`.
- **`enums.go`** — `MessageKind`, `EngageMode` (pattern|mention|mention-sticky), `SenderScope`, `IgnoredMessagePolicy`, `UnknownSenderPolicy`, `SessionMode`, `ChangeKind`, `Verdict`.
- **`rows.go`** — `MessageIn`, `MessageOut`, `ProcessingAck`, `Destination`, `SessionRouting`, `SessionState` (mirror the reference's observable schema semantics, not its names).
- **`schema.go`** — embedded SQL DDL constants for inbound and outbound, plus **pinned cipher params as constants** (`CipherPageSize=4096`, scheme name, raw-key mode). Pinning here guarantees host and sandbox compile byte-identical crypto (a mismatch = silent decrypt failure).
- **`crypto.go`** — `type SessionKey [32]byte`; `OpenInboundRO`, `OpenOutboundRW`, `OpenOutboundRO` centralize the exact connection string + PRAGMA ordering so neither side drifts (see Technical specs §1).
- **`queue.go`** — interface-segregated access; read-only-inbound enforced at the type level:

```go
// Sandbox is handed ONLY these two. No method writes inbound.
type InboundReader interface {
    PendingMessages(firstPoll bool) ([]MessageIn, error)
    Destinations() ([]Destination, error)
    SessionRouting() (SessionRouting, error)
    Close() error
}
type OutboundWriter interface {
    WriteMessageOut(MessageOut) error
    MarkProcessing(ids []MessageID) error
    MarkCompleted(ids []MessageID) error
    PutSessionState(key, value string) error
    Close() error
}
// Host gets the mirror images.
type InboundWriter interface {
    WriteMessageIn(MessageIn) error
    UpsertDestinations([]Destination) error
    MarkDelivered(id MessageID, platformMsgID *string) error
    Close() error
}
type OutboundReader interface {
    DueMessages() ([]MessageOut, error)
    ProcessingAcks() ([]ProcessingAck, error)
    Close() error
}
```

- **`gateway.go`** — the mandatory-mutation protocol types:

```go
type ChangeRequest struct {
    ID            ChangeID
    Kind          ChangeKind        // persona|enabled_tools|packages|wiring|permissions|mounts
    AgentGroupID  AgentGroupID
    RequestedBy   UserID
    Before, After json.RawMessage   // canonicalized (sorted keys) => deterministic diff/hash
    CreatedAt     time.Time
}
type Verdict int // VerdictPass | VerdictReject | VerdictRequireHuman
type Verifier interface {               // DETERMINISTIC, never an LLM
    Name() string
    Verify(ctx context.Context, req ChangeRequest) (Verdict, string, error)
}
type Decision struct { Outcome string; DecidedBy UserID; DecidedAt time.Time }
type Approver interface {
    RequestDecision(ctx context.Context, req ChangeRequest, reason string) (Decision, error)
}
type Applier interface {                 // idempotent, keyed by req.ID, transactional
    Apply(ctx context.Context, req ChangeRequest, d Decision) error
}
type ChangeStore interface {             // persists lifecycle; survives restart
    Put(ChangeRequest) error
    SetDecision(ChangeID, Decision) error
    MarkApplied(ChangeID) error
    Pending() ([]ChangeRequest, error)
}
```

## Agent 1 — Control plane (host). Owns `internal/host/**`, `cmd/controlplane`, `cmd/ironctl`, `api/`, `deploy/`

Per-package skeleton stubs (each ships with a package doc comment, the key interface/struct, and `// AGENT1: implement` TODOs):

- **`host/api`** — control-plane HTTP API; **bind only to the Tailscale interface** (see §5). Endpoints for submitting gateway changes, listing pending approvals, recording decisions, session/registry queries. `ironctl` is a thin client.
- **`host/gateway`** — implements `VerifierChain`, `Approver`, `Applier`, `ChangeStore` over the contract types. **v1 ships one verifier `AlwaysRequireHuman`** so every mutation hits a human; future verifiers (schema validator, mount-allowlist checker, policy engine) append to the chain and can only *add* rejections/human-gates. This is the single choke point for persona/tools/packages/wiring/permissions/mounts — there is no file-edit path.
- **`host/isolation`** — `Isolator` interface + `RunscIsolator` (containerd client, runtime `io.containerd.runsc.v1`). Sets OCI spec: inbound `ro` bind, outbound `rw`, **`network=none`**, drop all caps, `no_new_privs`, non-root userns, read-only rootfs + small writable `/workspace`. Mounts the model-proxy unix socket in (§4). Stub the Kata backend behind the same interface with a `// future` note.
- **`host/router`** — inbound routing: messaging-group resolution, fan-out to wired agent groups, engage-mode evaluation, session resolution, sender/access gating. Writes inbound via `InboundWriter`. (Mirror reference *semantics*; **fix the identity-spoofing bug**: always namespace `userId = channelType + ":" + handle`, never trust an embedded colon.)
- **`host/delivery`** — polls outbound via `OutboundReader`, delivers through channel adapters, dedups in inbound `delivered` (host never writes outbound). System actions are re-authorized host-side (no blind trust). **No unapproved `script`/RCE path** — any such action routes through the gateway.
- **`host/sweep`** — periodic stale-sandbox detection (heartbeat file mtime), due-message wake, recurrence, orphan reset with backoff.
- **`host/keys`** — per-session `SessionKey` generation, custody (host keystore encrypted under a host master key), and secure hand-off to the sandbox at launch (key delivered via a tmpfs/early-fd mechanism, never an env var). The sandbox image never contains a key.
- **`host/channels`** — adapter registry + a `fake` adapter for tests. Concrete platform adapters are out of scope for the skeleton (one stub adapter only).
- **`host/modelproxy`** — host-side model egress proxy: listens on a unix socket bound into the sandbox, forwards to the model API with an allowlist; the single outbound path. Stub with allowlist + forward + TODO for cap/log/redact.
- **`cmd/controlplane`** — wires the above; `cmd/ironctl` — admin CLI; `api/` — interface definitions; `deploy/` — install scripts for containerd+runsc, tailscale, systemd unit, with firewall defaults for admin access.

## Agent 2 — Sandbox. Owns `internal/sandbox/**`, `cmd/sandbox`

- **`sandbox/queue`** — implements `InboundReader` over `contract.OpenInboundRO` and `OutboundWriter` over `contract.OpenOutboundRW`. **Reopen the inbound handle every poll** (`mmap_size=0`, `query_only`) to defeat guest page cache; exit-on-corruption-streak so the host respawns with a fresh mount.
- **`sandbox/loop`** — the reasoning poll loop: read pending, format prompt, call provider, parse the model's structured output into outbound writes, mark processing/completed, heartbeat (touch `/workspace/.heartbeat`). Port the reference's poll-loop *semantics* (trigger=0 accumulate, follow-up polling during streaming, slash-command handling).
- **`sandbox/provider`** — `Provider` interface + `AnthropicProvider` (Messages API, tool use, streaming). The HTTP client points at the **host model-proxy unix socket**, not the public internet.
- **`sandbox/tools`** — in-sandbox tool implementations. **No `install_packages`/`add_mcp_server`/self-edit tools** — capability changes are control-plane mutations and only happen via the host gateway. Tools that need privilege emit a gateway change request, never act directly.
- **`cmd/sandbox`** — entrypoint: receive session key + paths, construct queue, run loop.

## Coordination protocol (so the two agents never collide)

- **Disjoint trees:** Agent 1 only touches `internal/host/**` + its `cmd/*` + `api/` + `deploy/`. Agent 2 only touches `internal/sandbox/**` + `cmd/sandbox`. Neither edits the other's tree.
- **Frozen seam:** `internal/contract/**` is authored in this skeleton and frozen. Any change needs an RFC entry in `docs/contract.md` + both CODEOWNERS approvals. Enforced by `CODEOWNERS`.
- **Shared but additive:** `test/parity/**` — both may add specs; the `harness/` sub-package is owned by Agent 1 (it spins up the host) but exposes a documented fake-sandbox hook Agent 2 uses.
- **Each stub file** carries a header banner: `// OWNER: AGENT1` or `// OWNER: AGENT2`, and `// CONTRACT: read-only import` where relevant.

## Key technical specifications

**§1 Encrypted read-only inbound (the load-bearing detail).** Open with `file:<path>?mode=ro&_busy_timeout=5000`; then, in order on the fresh handle: cipher pragmas → `PRAGMA key = "x'<64hex>'"` (raw key, before any page read) → `PRAGMA query_only=ON` → `PRAGMA mmap_size=0`. **Never `immutable=1`** (the file changes; it would corrupt/freeze reads). DELETE journal mode (not WAL — WAL `-shm` mmap doesn't refresh across the bind mount). Reopen per poll. Outbound: sandbox opens RW with `journal_mode=DELETE`; host reads it `mode=ro` with the same reopen discipline. Host is sole writer of inbound, sandbox sole writer of outbound.

**§2 Gateway determinism.** Verdicts are pure functions of `ChangeRequest`; the human step is a recorded boolean, not in-system judgment. "Auto-check before human" = a verifier returning `RequireHuman` on pass. "Auto-approve low-risk kinds later" = a config flag permitting auto-apply only when the chain is all-`Pass` for specific `ChangeKind`s. Approver/Applier never change to extend verification.

**§3 Isolation.** containerd Go client; `containerd.WithRuntime("io.containerd.runsc.v1", nil)`. OCI: inbound `ro` bind, outbound `rw`, model-proxy socket bind, `network=none`, caps dropped, `no_new_privs`, non-root user, ro rootfs. runsc platform configurable (`systrap` default; `ptrace` where no `/dev/kvm`).

**§4 Model egress.** Host `modelproxy` listens on a unix socket; `RunscIsolator` binds that socket into the sandbox; `AnthropicProvider` dials it. Sandbox has no other network. Host enforces destination allowlist.

**§5 Mesh.** `host/api` binds only to the Tailscale interface address; `deploy/` ships tailnet + firewall defaults so the control-plane API has no public port. Agent egress firewalling is documented in `docs/architecture.md` as a future extension.

**§6 Parity tests.** Black-box specs over the observable surfaces (the two queue DBs + control-plane API) only. **No import or naming of the reference.** Each test's doc comment states the behavioral contract as prose. Families: routing fan-out, engage modes, session resolution, delivery dedup, gateway mandatory-approval, and a **cross-mount live-poll** spec (write inbound after the sandbox is polling; assert observed within one interval) — the encrypted-DELETE + `mmap_size=0` validation.

## What the skeleton deliverable contains

- A repo that **`go build ./...` and `go vet ./...` succeed** on (CGo enabled for the SQLite3MC build).
- `internal/contract` **fully authored** (real types/interfaces/DDL/pragyma helpers), not stubs.
- Every `internal/host/**` and `internal/sandbox/**` package present with: package doc, the primary interface/struct, owner banner, and `// AGENTx: implement` TODOs — compiling no-op implementations (return `errors.New("not implemented")`).
- `cmd/*` entrypoints that wire dependencies and compile.
- OSS scaffolding: `LICENSE` (MIT), `README.md` (threat model + quickstart), `CONTRIBUTING.md` (freeze rule), `CODEOWNERS`, `Makefile`, `.github/workflows/ci.yml`, `docs/*`.
- `test/parity/harness` + one passing smoke spec and the rest as `t.Skip("AGENTx: implement")` placeholders so CI is green.

## Verification

1. `make build` (or `CGO_ENABLED=1 go build ./...`) — whole tree compiles, including the CGo SQLite3MC binding.
2. `go vet ./...` clean; `make lint` clean.
3. `go test ./internal/contract/...` — crypto round-trip test: write an encrypted DB with a key, reopen RO with the same key (reads succeed), reopen with a wrong key (fails with `NOTADB`), attempt a write through `InboundReader` (no method; and `query_only` blocks it at runtime).
4. `go test ./test/parity/...` — smoke spec passes; skipped specs report as TODO.
5. Manual: `cmd/controlplane` boots, binds only to the Tailscale address, exposes the gateway API; `ironctl` submits a `ChangeRequest` and sees it held pending a human decision (gateway choke point works end to end with the `AlwaysRequireHuman` verifier).
6. CI (`.github/workflows/ci.yml`) runs steps 1–4 on push.

## Build order after skeleton

1. Land skeleton + frozen contract (this plan).
2. Agent 1 and Agent 2 implement their trees in parallel against the contract.
3. Integration: real `RunscIsolator` + live-poll parity spec (the riskiest cross-mount check) before any feature work.
