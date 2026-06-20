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
  source commit and the workflow that built it.

The [Release runbook](release-runbook.md) is the operational reference for cutting,
verifying, and yanking a release.

## Reporting a vulnerability

Disclosure policy lives in
[SECURITY.md](https://github.com/IronSecCo/ironclaw/blob/main/SECURITY.md). The
scope — what counts as a vulnerability — is defined in §8 of the
[threat model](threat-model.md), which the disclosure policy points back to.
