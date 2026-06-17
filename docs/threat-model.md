# Threat model

IronClaw assumes the agent inside the sandbox is potentially compromised — by
prompt injection or otherwise — and designs the trust boundary so that a
compromised agent cannot escalate.

## Threats and mitigations

| Threat | Mitigation |
|--------|------------|
| Agent reads or edits its own runtime source | Compiled Go binary; no interpreter in the sandbox. |
| Agent self-modifies config (persona, tools, packages, wiring, permissions, mounts) | All mutations flow through the mandatory gateway; deterministic verifier chain → human approval → idempotent apply. No file is the source of truth. |
| Prompt injection reaches a privileged action (e.g. RCE via an unapproved `script` field) | No unapproved action path exists; privileged needs emit a gateway ChangeRequest. System actions are re-authorized host-side. |
| Disk theft / cross-session reads of queues | Per-session encrypted SQLite (raw-key, pinned cipher params). |
| Sandbox writes the inbound queue it should only read | Interface segregation (no write method), `PRAGMA query_only`, and a read-only OS bind mount — three independent layers. |
| Sandbox makes arbitrary network calls | `network=none` (no NIC inside the sandbox). The sandbox reaches the host only over unix sockets: the model-proxy (model-host allowlist) and — when enabled — the egress broker (deny-by-default external-API allowlist + per-request audit). See "Egress broker" below. |
| Public exposure of the control-plane API | API binds only to the Tailscale interface; no public port. |
| Sandbox escape | gVisor (runsc): dropped caps, `no_new_privs`, non-root userns, read-only rootfs. |

## Egress broker (T-111)

The egress broker (`internal/host/egress`) lets an agent reach **operator-approved
external APIs** beyond the model host. It is a deliberate, bounded relaxation of
the original "model host only" posture, designed to keep the trust boundary
intact:

- **The sandbox stays `network=none`.** It never gets a NIC. The broker is a
  second host unix socket bound into the sandbox (alongside the model-proxy
  socket); every byte still crosses a host choke point. The egress socket is
  bound **only** when `SandboxSpec.EgressSocket` is set, so the default
  (`HardenedSpec`) remains sealed to the model proxy alone.
- **Deny by default.** The broker forwards only to hosts on an explicit
  allowlist. An empty allowlist forwards nothing; an unapproved host returns 403.
  In production the allowlist is mutated only after the change clears the
  gateway's human approval (wired by the daemon, T-120).
- **Audited.** Every request — allowed or denied — emits an audit record (host,
  path, method, status, byte counts, duration). Allowlist additions/removals are
  logged.
- **HTTPS only**, and **not a credential vault** (an explicit IronClaw non-goal):
  the broker injects no host-held secrets and forwards the request's own headers,
  so it cannot launder access to a credential the sandbox does not already hold.
- **Agent surface:** the opt-in `http_fetch` sandbox tool, registered only when an
  egress socket is configured.

**Threat-model review sign-off:** reviewed and approved by the maintainer (sole
CODEOWNER, `@ToPmit26`) on 2026-06-16, via the decision to un-gate T-111 and wire
egress live. Residual risk accepted: a compromised agent can exfiltrate to / pull
from any *approved* host; this is mitigated by deny-by-default approval and full
per-request audit, and bounded by keeping the sandbox itself network-less.

## Out of scope (documented future work)

Per-host egress rate caps and response secret redaction for the egress broker
(the model-proxy hardening pattern, T-107, applied to egress); a Kata isolation
backend; automated (non-human) gateway approval for low-risk change kinds.
