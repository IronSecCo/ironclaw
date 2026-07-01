# Security Policy

IronClaw is a security-first platform: it runs potentially-compromised AI agents inside hardened,
sandboxed environments and gates every privileged action behind a human-approval control-plane. We hold
ourselves to that same bar. Responsible reports from the community are the most important input to
keeping IronClaw trustworthy, and we want reporting to be fast, private, and low-friction.

## Supported versions

IronClaw is **pre-1.0**. Releases are cut as `v0.1.<commit-count>` on every push to `main`. Until a 1.0
line exists we support only the **latest release** — please reproduce against it before reporting.

| Version | Supported |
|---|---|
| Latest `v0.1.x` release | ✅ |
| Older `v0.1.x` releases | ❌ (upgrade to latest) |
| `main` (unreleased) | ✅ best-effort |

## Reporting a vulnerability

**Please do not open a public issue, discussion, or pull request for a security vulnerability.** Public
disclosure before a fix is available puts every user at risk.

Use one of these private channels instead:

1. **GitHub Private Vulnerability Reporting (preferred).** Go to the repository's
   **[Security tab → "Report a vulnerability"](https://github.com/IronSecCo/ironclaw/security/advisories/new)**.
   This opens a private advisory only you and the maintainers can see, and keeps the whole
   coordination — discussion, fix, CVE, credit — in one place.
2. **LinkedIn.** Message a maintainer — Omer Zamir
   (<https://www.linkedin.com/in/omerzamir>) or Topaz Aharon
   (<https://www.linkedin.com/in/topaz-aharon/>). Please include `IronClaw security` in the
   first message so it is triaged quickly, and don't post vulnerability details publicly.

### What to include

A good report lets us reproduce and triage quickly. Where possible:

- The affected version / commit SHA and platform.
- The trust boundary involved (see [`docs/threat-model.md`](docs/threat-model.md)) — e.g. sandbox→host
  escape, control-plane API, gateway bypass, egress broker, agent-to-agent.
- Step-by-step reproduction, a proof-of-concept, and the impact you can demonstrate.
- Any logs, crash output, or configuration needed to reproduce.

## Our commitment (coordinated disclosure)

| Stage | Target |
|---|---|
| Acknowledge your report | within **3 business days** |
| Initial assessment + severity | within **14 days** |
| Status updates | at least every **14 days** until resolved |
| Coordinated public disclosure | within **90 days**, or sooner once a fix ships — by mutual agreement |

We will work with you on a disclosure timeline, credit you in the advisory and release notes (unless you
prefer to remain anonymous), and request a CVE where warranted. If we cannot meet a timeline we will tell
you why.

## Safe harbor

We consider security research conducted in good faith under this policy to be authorized, and we will not
pursue or support legal action against you for it, provided you:

- Make a good-faith effort to avoid privacy violations, data destruction, and service degradation.
- Only interact with accounts/systems you own or have explicit permission to test.
- Do not exploit a finding beyond the minimum needed to demonstrate it, and do not access, modify, or
  retain others' data.
- Give us a reasonable time to remediate before any public disclosure.

If in doubt about whether an action is authorized, ask us first at the contact above.

## Scope

The **authoritative** definition of what does and does not count as a vulnerability lives in the
[threat model](docs/threat-model.md) — **§8 "What counts as a vulnerability"** and **§9 "Non-goals"**.
That document is the single source of truth for the threat model; this policy defers to it rather than
keeping a second copy that could drift. In brief:

**In scope** — anything that crosses one of the threat model's trust boundaries (§3, §5): sandbox→host
escape, bypassing the mandatory human-approval gateway, reading another session's encrypted queues or
recovering session/master keys, egress-broker / model-proxy / agent-to-agent allowlist bypass,
control-plane authentication/authorization/RBAC bypass, secret leakage in logs or forwarded responses,
and supply-chain weaknesses in the release/build pipeline. The exact list is
[§8](docs/threat-model.md#8-what-counts-as-a-vulnerability).

**Out of scope** — the intentional non-goals of the sealed / `network=none` design (in-sandbox
browser/network access, `install_packages`/self-modification, an in-broker credential vault), plus
findings that require a malicious operator/maintainer, a compromised host, or physical access;
volumetric DoS; best-practice nitpicks without demonstrated impact; scanner output without a working
proof-of-concept; and social engineering. See [§8–§9](docs/threat-model.md#8-what-counts-as-a-vulnerability)
for the reasoning.

## Verifying a release you received

Every release is checksummed, keyless-signed with cosign, and carries build-provenance attestations —
and also ships a **signed containment report** (`ironclaw_<version>.containment.json` / `.txt`) that
machine-verifies the core sandbox invariants (§5/§8) held for that exact commit and the runtime tested.
The step-by-step verification (checksums, signature, provenance, and the containment report) is in the
[release runbook](docs/release-runbook.md#4-how-to-verify-a-release-user-facing).

Thank you for helping keep IronClaw and its users safe.
