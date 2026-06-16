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
| Sandbox makes arbitrary network calls | `network=none`; model calls go only through the host model-proxy unix socket with a destination allowlist. |
| Public exposure of the control-plane API | API binds only to the Tailscale interface; no public port. |
| Sandbox escape | gVisor (runsc): dropped caps, `no_new_privs`, non-root userns, read-only rootfs. |

## Out of scope (documented future work)

Agent egress firewalling beyond the model-proxy allowlist; a Kata isolation
backend; automated (non-human) gateway approval for low-risk change kinds.
