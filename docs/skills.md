---
title: "Skills: declarative capability bundles, no in-sandbox code"
description: Skills are declarative capability bundles for IronClaw agents — persona, tool subset, egress allowlist — composed through the human-approval gateway, never new code in the sandbox.
---

# Skills

A **skill** is a declarative, host-curated **capability bundle** — a persona
fragment, an enabled subset of the *compiled* sandbox tools, a set of approved
egress hosts, and read-only reference assets. A skill is **never executable code
that runs in the sandbox**.

This is the central design choice: installing a skill cannot introduce new code.
It can only *compose capabilities the binary already implements*, and only through
the gateway's human-approval flow. That preserves IronClaw's sealed-runtime
pillar — no interpreter, no in-sandbox install, no rootfs mutation.

> Source: [`internal/host/skills/`](https://github.com/IronSecCo/ironclaw/tree/main/internal/host/skills)
> (`manifest.go` is the schema + loader, `install.go` maps an install to a
> gateway `ChangeRequest`).

## The manifest

A skill is described by a `skill.yaml` manifest. The only schema version v1
accepts is `ironclaw.dev/skill/v1`. The loader parses and validates it into a
typed `Manifest`, **failing closed** on anything malformed or out of policy.

```yaml
apiVersion: ironclaw.dev/skill/v1
name: web-research
version: 1.2.0
description: Curated web-research capability for an agent group.
grants:
  persona: |
    You can research topics on the public web and cite your sources.
  tools:
    - web_search          # must be a tool the binary already compiles in
    - http_get
  egress:
    - duckduckgo.com
    - api.search.brave.com
  assets:
    - reference/search-operators.md   # read-only, mounted noexec
signature: <provenance attestation over (manifest + asset tree)>
```

Every ingredient under `grants` maps to an existing gateway-governed mechanism:

| Manifest field | What it grants | Enforced by |
| --- | --- | --- |
| `persona` | A persona fragment added to the agent group | Gateway change-approval |
| `tools` | An enabled subset of the **already-compiled** sandbox tools | Tool allow-list |
| `egress` | Approved outbound hosts | Egress broker (sandbox stays `network=none` by default) |
| `assets` | Read-only reference files mounted at `/skills/<name>` | Mount is `nosuid,nodev,noexec` |

There is deliberately **no** `command`, `script`, or `rootfs` field. An approved
install can only ever touch configuration.

## Signatures are recorded, not trusted

The `signature` field is a provenance attestation over the manifest plus its asset
tree. The loader **records** it but does not trust it — it is verified separately
against a host-configured trust root. A malformed or unsigned manifest does not
silently become trusted.

## Installing a skill is a set of approvals

Installing a skill is not a privileged side-channel. `install.go` turns a skill
into a gateway `ChangeRequest` whose `After` payload (`SkillInstall`) is the exact,
human-readable bundle the approver sees: which persona text, which tools, which
egress hosts, and which read-only asset mount the skill wants — **and nothing
else**.

So a skill install flows through the same human-approval gateway as every other
capability change (see the [Quickstart](quickstart.md) for the gateway in action),
and lands in the same [audit log](architecture.md). One skill → one mount at
`/skills/<name>`, enforced `noexec`.

## Why this shape

The reference design IronClaw was built to harden wired extensions with a *blind*
approval surface — "approve this bundle" and it brings whatever it likes. IronClaw
closes that gap the same way it closes it for [MCP servers](mcp.md): a human
approves a **named** capability set, every grant is explicit and gated, and no part
of a skill ever executes inside the sandbox.
