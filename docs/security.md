---
title: "Security & trust: gVisor sandbox + approval gateway"
description: How IronClaw's security holds — per-session gVisor sandboxes with network=none, a no-bypass human approval gateway, and cosign-signed, reproducible releases you can verify.
---

# Security & trust

IronClaw's value is not "AI agents" — it is **AI agents you can run without
trusting them**. This page is the map of that trust story: what IronClaw defends
against, the invariants that make the defense hold, and how *you* verify what you
install.

## Start here

<div class="grid cards" markdown>

-   :material-shield-bug: **[Threat model](threat-model.md)**

    The assumption that drives every design decision: the agent inside the
    sandbox is *potentially compromised*. What that means for the blast radius,
    and — in §8 — what counts as a vulnerability.

-   :material-lock-check: **[Verify a release](release-runbook.md#4-how-to-verify-a-release-user-facing)**

    Every release is checksummed, keyless-signed with cosign, and carries
    build-provenance attestations. Here is how to check all three before you
    trust a download.

</div>

## The invariants

IronClaw's hardening rests on a small set of invariants that hold regardless of
what the agent does:

- **Sealed runtime.** The sandbox has no interpreter, no in-sandbox package
  install, and no rootfs mutation. An agent can only invoke capabilities the
  binary already compiled in. This is why [skills](skills.md) and
  [MCP servers](mcp.md) grant capabilities through configuration, never code.
- **Deterministic approval gateway.** Every mutation — persona, enabled tools,
  packages, wiring, permissions, mounts, `create_agent` — is *held* at the
  gateway until a human approves it. There is no bypass path. The
  [Quickstart](quickstart.md) makes this choke point concrete in two commands.
- **No public surface.** The control-plane API binds only to the private mesh
  (Tailscale) interface; network reachability is the primary control, and the
  bearer token is defense-in-depth on top of it. See the
  [API reference](reference/api.md).
- **Network-isolated sandboxes.** Sandboxes run `network=none` by default;
  outbound access is granted host-by-host through the egress broker, only as
  part of an approved change.
- **Append-only audit.** Every approve/reject decision and every gated action is
  recorded. See [Architecture](architecture.md).

## Credential vault: agents use keys without holding them

An agent reaches a vaulted API by **logical name** — `vault://<cred>/<path>` — and
never by holding the key. The egress broker forwards the call to a separate host-side
*injector* that attaches the real credential; the broker (and the sandbox) inject
nothing. Access is **deny-by-default and per agent group**: a group may use a
credential against a host only if an approved policy grant says so.

Those grants are **config, never secrets** — every rule names a credential, never
holds one — and they are managed through the gateway like any other capability
change, so a grant is held until a human approves it and is recorded in the audit
log. Manage them with `ironctl vault`:

```bash
# See a group's deny-by-default state and active grants (no secret is ever shown):
ironctl vault list --group <agent-group>

# Propose a grant (held at the gateway for human approval):
ironctl vault grant  --group <agent-group> --credential github --host api.github.com --by you
ironctl change approve <change-id> --by you

# Narrow or remove a grant (also gateway-gated):
ironctl vault revoke --group <agent-group> --credential github --host api.github.com --by you
```

Rotating the **secret value** behind a credential is an *injector* operation — the
control plane never holds the key, so there is nothing for it to rotate. Point the
broker at an injector with `--vault-endpoint`. The threat model's §11 has the full
model.

## The supply chain is part of the promise

A release a user cannot verify is not a secured release. IronClaw's published
artifacts are:

- **Reproducible** — re-derivable from a known commit, with no nondeterministic
  inputs leaking into the build.
- **Checksummed** — every archive is listed in `SHA256SUMS`; the installers
  (`install.sh` / `install.ps1`) verify checksums *before* executing anything.
- **Signed** — a keyless [cosign](https://github.com/sigstore/cosign) signature
  over `SHA256SUMS` is the trust anchor.
- **Attested** — build-provenance attestations tie every artifact back to its
  source commit and the workflow that built it. Both the release archives and the
  individual binaries inside them carry provenance, so `gh attestation verify`
  works against either.

The [Release runbook](release-runbook.md) is the operational reference for cutting,
verifying, and yanking a release.

## Reporting a vulnerability

Disclosure policy lives in
[SECURITY.md](https://github.com/IronSecCo/ironclaw/blob/main/SECURITY.md). The
scope — what counts as a vulnerability — is defined in §8 of the
[threat model](threat-model.md), which the disclosure policy points back to.
