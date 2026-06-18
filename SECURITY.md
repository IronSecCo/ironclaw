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
   **[Security tab → "Report a vulnerability"](https://github.com/nivardsec/ironclaw/security/advisories/new)**.
   This opens a private advisory only you and the maintainers can see, and keeps the whole
   coordination — discussion, fix, CVE, credit — in one place.
2. **Email.** `security@nivardsec.com`. Encrypt with our PGP key if you wish — request it at that
   address, or use the key fingerprint published in the GitHub advisory page. Please include
   `IronClaw security` in the subject.

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

**In scope** — anything that breaks IronClaw's intended trust boundaries, including:

- Sandbox escape (gVisor/Kata) or any path that lets a sandboxed agent reach the host beyond the
  sanctioned unix sockets.
- Bypassing the **mandatory human-approval gateway** (applying a capability change without approval).
- Reading another session's encrypted queues, or recovering session/master keys.
- Egress-broker or model-proxy allowlist bypass; agent-to-agent (a2a) hop/quota/permission bypass.
- Control-plane API authentication, authorization, or RBAC bypass.
- Secret/credential leakage in logs, audit records, or forwarded responses.
- Supply-chain weaknesses in our release/build pipeline.

**Out of scope** — intentional non-goals of the sealed / `network=none` design (see
[`docs/threat-model.md`](docs/threat-model.md)), plus the usual exclusions:

- Requests to add in-sandbox browser/network access, `install_packages`/self-modification, or a general
  arbitrary-API credential vault — these are deliberate design decisions, not vulnerabilities.
- Findings that require a malicious operator/maintainer, physical access, or a compromised host OS
  (IronClaw's threat model assumes a trusted host and a potentially-hostile agent).
- Volumetric DoS, rate-limiting nitpicks, missing best-practice headers without demonstrated impact,
  and automated-scanner output without a working proof-of-concept.
- Social engineering of maintainers or users.

Thank you for helping keep IronClaw and its users safe.
