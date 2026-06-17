# Spike T-227 — Host-side skills / extension system

> **Status:** recommendation (design only — no production code).
> **Gap:** G-036 — no skills/extension system. Peers' headline extensibility
> (openclaw `SKILL.md` + ClawHub, 13.7k+ skills; nanoclaw `/add-*` branch-copy)
> sits directly against IronClaw's *sealed runtime / no in-sandbox install* pillar.
> **Feeds:** the follow-up task breakdown in §7 (skill manifest, capability-grant
> mapping, asset mount, curated registry, `ironctl skill`).
> **Decision:** **BUILD — host-side, gateway-gated capability bundles only.**
> Approved by the maintainer (needs-human) on 2026-06-17.
> **Author:** claude-Omers-MacBook-Pro · **Base-SHA:** ae0ff7e

---

## 1. The tension

Both peers treat *skills* as their headline extensibility, and both do it the way
IronClaw deliberately refuses to:

- **openclaw** — a `SKILL.md` manifest plus arbitrary bundled scripts, installed
  from **ClawHub** (a public registry of 13.7k+ skills) and executed inside the
  agent's runtime.
- **nanoclaw** — `/add-*` commands that branch-copy code into the agent tree and run
  it in-process.

Both designs share two properties that IronClaw's threat model treats as the
*primary* attack surface, not a feature:

1. **In-runtime code execution** — a skill is (or carries) code that runs with the
   agent's privileges.
2. **Self-service install** — the agent (or an unreviewed marketplace fetch) adds a
   capability without a human in the loop.

This is not hypothetical. Koi Security catalogued **341 malicious skills on ClawHub**
— typosquats and trojaned bundles that exfiltrated secrets or ran post-install
hooks the moment they were added. A registry that combines *auto-install* with
*code execution* is the exact supply-chain vector IronClaw was built to remove
(see [threat-model.md](../../docs/threat-model.md) §1, B1-T "Agent edits its own
runtime/source", and §9 "package installation / self-modification" non-goal).

So the question is not "skills: yes or no" — it is **"can we deliver the *value* of
skills (reusable, shareable capability bundles) without reintroducing either of the
two dangerous properties?"** This spike concludes: **yes, if a skill is host-side
config that composes already-implemented capabilities under human approval, and
never code that runs in the sandbox.**

## 2. What a skill must be in IronClaw's model

The reframing that makes skills safe here: **a skill is a host-curated *capability
bundle*, not a unit of code.** It declares intent; it never ships a runtime.

A skill bundles three kinds of thing, all of which IronClaw already has a governed
mechanism for:

| Skill ingredient | Existing IronClaw mechanism it maps to |
|---|---|
| A persona / prompt fragment | the `persona` change-kind (gateway-approved) |
| A set of tools the skill needs enabled | the `tools` change-kind — tools are **compiled into the sandbox image** already; a skill only *enables* a subset, it cannot add a new tool binary |
| Approved external hosts the skill calls | the egress broker allowlist (T-111), mutated only via the gateway |
| Read-only reference assets (templates, schemas, docs) | a read-only `/skills/<name>` bind mount, alongside the existing `/shared` mount pattern (`SandboxSpec.SharedReadOnlyPath`) |

Crucially, **nothing in that list is executable code introduced by the skill.** The
tools a skill "adds" already exist in the compiled `/sandbox` binary
(`cmd/sandbox/buildTools`); a skill can only switch already-present capabilities on
for an agent group and point them at approved destinations. There is no
`install_packages`, no script, no interpreter — the read-only rootfs and the
compiled-Go / no-interpreter invariant (B1-T) are untouched.

## 3. Manifest format

A skill is a signed manifest plus an optional read-only asset directory. The
manifest is declarative and capability-explicit — every privilege it wants is
named so a human can see it at approval time:

```yaml
# skill.yaml
apiVersion: ironclaw.dev/skill/v1
name: incident-triage
version: 1.4.0
description: Triage PagerDuty alerts and draft a status-page update.
author: nivardsec
# What the skill contributes to an agent group, all gateway-approved on install:
grants:
  persona: |            # appended to the group persona (a persona change-kind)
    You are an on-call triage assistant. Be terse, cite alert IDs.
  tools:                # MUST be a subset of the compiled tool registry; no new code
    - http_fetch
    - send_message
    - schedule
  egress:               # becomes egress-broker allowlist entries (deny-by-default)
    - api.pagerduty.com
    - status.example.com
  assets:               # bundled read-only files, mounted at /skills/incident-triage
    - templates/status-update.md
    - runbooks/sev1.md
# Provenance — verified host-side before the manifest is ever shown for approval:
signature: cosign|minisign over the (manifest + asset tree) digest
```

Validation rules the host enforces *before* an install ChangeRequest is even
created (fail-closed):

- `tools` ⊆ the compiled tool registry. An unknown tool name is rejected — a skill
  can never name a capability the binary does not already implement.
- `egress` entries are hostnames only (no wildcards in v1); each becomes an explicit
  allowlist entry the human sees and approves.
- `assets` are bound **read-only**, `nosuid,nodev,noexec`, outside the rootfs — they
  are data, never executed.
- `signature` verifies against a host-configured trust root, or the skill is
  refused (see §5).

## 4. Where skills live, and how install maps to the gateway

**Install is a gateway ChangeRequest, never a sandbox action.** The flow:

```
ironctl skill add incident-triage@1.4.0
        │  (host-side CLI; the sandbox agent cannot trigger this)
        ▼
  host fetches + verifies signature + validates manifest (§3)
        ▼
  host synthesizes ONE ChangeRequest bundling the declared grants
  (persona += …, tools += …, egress += …, mount /skills/incident-triage)
        ▼
  gateway verifier chain → AlwaysRequireHuman floor → human approves/rejects
        ▼  (on approve)
  idempotent apply: registry + egress allowlist + mount allowlist updated
```

This reuses the existing machinery wholesale — `internal/host/gateway`'s verifier
chain, `MountAllowlistVerifier`, the egress allowlist mutation path (T-111/T-120),
and the manual-approval floor. A skill install is just a *bundled, pre-validated*
capability change. The human approving it sees exactly which tools, which egress
hosts, and which mounts the skill wants — the approval UI already renders
ChangeRequests.

Two hard guarantees fall out for free:

- **No self-service.** The trigger is `ironctl` (host/admin), not a sandbox tool.
  Even if we later add a sandbox-side `request_skill` tool, it would emit a
  ChangeRequest — i.e. land on the same human-approval floor as `create_agent`
  (RFC-0004). The agent can *ask*; only a human can *grant*.
- **No new code path.** The apply step touches config (registry/allowlists/mounts)
  only. It never writes to the rootfs or adds an executable.

## 5. Trust model for third-party skills

Third-party skill content is **untrusted by default** — the same posture as an
inbound chat message or an egress response (threat-model §1). The controls:

1. **Curated host registry, not an open marketplace.** Skills resolve from a
   host-configured source (a pinned Git ref or an OCI registry the operator
   controls), not an arbitrary URL the agent supplies. The ClawHub lesson is that
   *open + auto-install* is the vector; we keep neither half.
2. **Signature verification before display.** A skill whose signature does not
   verify against the configured trust root is refused at fetch time — it never
   reaches the approval step. (cosign/minisign; the exact tool is a follow-up
   decision, but the gate is mandatory.)
3. **Capability grants are explicit and human-approved.** Because a skill cannot run
   code, the *only* damage surface is the capabilities it requests — and every one
   of those is named in the manifest and shown to the human at install. A trojaned
   `incident-triage` that quietly asks for `egress: evil.example.com` is visible in
   the diff and rejected by the approver, not discovered post-breach.
4. **Least privilege at runtime, unchanged.** Even an approved skill runs inside the
   same `network=none`, read-only-rootfs, dropped-caps sandbox. Its egress is
   broker-mediated and audited; its assets are read-only. A malicious-but-approved
   skill is bounded by exactly the controls in threat-model §7/§10.

Net: the worst a hostile third-party skill can do is **request** privileges — which
a human sees and denies — and it can never execute code or self-install. That is a
categorically smaller surface than either peer's design.

## 6. Recommendation — BUILD (host-side, gated)

Build the host-side capability-bundle mechanism described above. It delivers the
*reusability and shareability* that make skills valuable, while preserving every
pillar of the sealed runtime:

- ✅ no in-sandbox install (install is `ironctl` + gateway)
- ✅ no code execution from skills (tools are pre-compiled; skills only enable them)
- ✅ no new egress without human approval (grants flow through the existing gateway)
- ✅ supply-chain risk addressed at the root (curated source + signatures + explicit,
  human-reviewed capability grants), directly answering the ClawHub failure mode

Rejected alternatives: **defer** (cedes the single most-cited extensibility gap with
no safe path forward — but the safe path exists, so deferring is leaving value on
the table); **integrate openclaw `SKILL.md` as-is** (its model assumes in-runtime
script execution, which we cannot adopt without breaking B1-T — we can borrow the
*manifest ergonomics* but not the execution model).

## 7. Follow-up task breakdown (if 'build')

| Task | Scope | Owned paths (proposed) |
|---|---|---|
| **T-227a** Skill manifest schema + loader | parse/validate `skill.yaml`, enforce §3 rules (tools ⊆ registry, hostnames, read-only assets) | `internal/host/skills/**` |
| **T-227b** Signature verification + curated source | fetch from pinned Git/OCI source; verify cosign/minisign against a host trust root; fail-closed | `internal/host/skills/**` |
| **T-227c** Install → ChangeRequest mapping | synthesize one bundled ChangeRequest (persona/tools/egress/mount) from a manifest; reuse the gateway chain + manual-approval floor | `internal/host/skills/**`, wiring in `cmd/controlplane` |
| **T-227d** Read-only skill asset mount | bind `/skills/<name>` read-only (`nosuid,nodev,noexec`), mirroring the `/shared` mount in `SandboxSpec` | `internal/host/isolation/**` |
| **T-227e** `ironctl skill add/list/remove` | host-side CLI surface; never a sandbox tool | `cmd/ironctl/**` |
| **T-227f** Threat-model addendum | document the skills boundary + sign-off (mirror §7/§10) | `docs/threat-model.md` |

None of these touch `internal/contract/**`. T-227c and T-227d are the only ones that
touch gateway/isolation and should be reviewed against the existing capability-change
and mount-allowlist invariants; the rest are additive host packages.
