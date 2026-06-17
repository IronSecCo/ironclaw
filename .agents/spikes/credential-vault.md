# Spike T-260 — End-user credential vault (request-time injection)

> **Status:** recommendation (design only — no production code).
> **Gap:** G-041 — no end-user credential vault. nanoclaw's headline secret story
> is OneCLI's **Agent Vault**: agents never hold raw keys; the gateway injects
> credentials at request time with per-agent policy + rate limits. IronClaw injects
> only the *model* credential and, by design, its egress broker (T-111) is **not a
> credential vault**.
> **Feeds:** the follow-up task breakdown in §8.
> **Decision:** **INTEGRATE — wrap OneCLI as the host-side vault; do NOT build a
> bespoke secret-injection path into the egress broker.** Building our own vault
> stays a non-goal. Approved by the maintainer (needs-human) on 2026-06-17.
> **Author:** claude-Omers-MacBook-Pro · **Base-SHA:** ae0ff7e

---

## 1. The gap

A long-running agent that talks to real third-party APIs (GitHub, PagerDuty,
Stripe, an internal service) needs credentials for them. Two ways to give it
those:

- **Hand the agent the keys** — the obvious approach, and the one IronClaw's whole
  threat model exists to prevent. A compromised agent (the *assumed* state, §1)
  with raw keys is an exfiltration event.
- **Never hand over keys; inject at request time** — nanoclaw's OneCLI Agent Vault.
  The agent calls an API by *name*; a host-side broker attaches the right
  credential, enforces a per-agent policy + rate limit, and audits the call. The
  agent never sees plaintext.

IronClaw already does the second pattern for exactly one secret — the model key,
via the model proxy (`internal/host/modelproxy`). The gap is generalizing it to
**arbitrary approved APIs**.

## 2. The constraint: our egress broker is deliberately *not* a vault

The natural place to "build" this is the egress broker (T-111,
[threat-model.md](../../docs/threat-model.md) §7). But the broker is explicitly
specified as **not** a credential vault, and that is a load-bearing property:

> B4-E: *"The broker injects **no** host secrets and forwards only the request's own
> headers — it cannot grant access the sandbox does not already hold. Not a
> credential vault (an explicit non-goal)."* — threat-model §5/§7

That sentence is what lets the egress broker be a small, auditable, deny-by-default
forwarder whose worst case is "exfiltration to an *approved* host" (a bounded,
accepted risk). The moment the broker starts injecting host-held secrets, its blast
radius changes qualitatively: a bug in host-matching or policy evaluation now leaks
a *real credential* to a wrong destination, not just the request's own bytes. So
"build the vault into the broker" is not a small extension — it reverses a
deliberate threat-model decision and enlarges the most security-sensitive choke
point we have.

That reframes the build-vs-integrate question: **do we want to design, implement,
and own a novel secret-injection-and-policy engine inside our minimal trusted
egress path — or wire in a purpose-built, already-audited vault that does exactly
this, keeping the secret-injection logic *outside* our broker?**

## 3. Option B — build request-time injection on the egress broker

What it would take (sketch): a host-side secret store (encrypted at rest, keyed by
the existing keystore master key), a per-agent-group policy model ("group X may use
credential Y against host Z, ≤N req/min"), and an injection step in the broker's
forwarding `Director` that attaches the credential when the request matches a
policy — plus redaction so the credential never echoes back (the model-proxy
hardening pattern, T-107, generalized).

**Cost / risk:**
- Re-opens B4-E: the broker becomes a secret sink; every forwarding bug is now a
  potential credential leak. New, broad threat-model review required.
- We own a bespoke policy engine and secret store — exactly the kind of
  security-critical, easy-to-get-subtly-wrong component that benefits most from
  reuse and external scrutiny (cf. T-259, the audit engagement).
- Duplicates capability that a dedicated vault already ships (per-agent policy +
  rate limit + audit), for no differentiation — IronClaw's value is the *sandbox
  seal*, not a novel vault.

It is buildable, and the architecture is known, but the cost is "own a new piece of
trusted secret-handling infrastructure" for a P3 parity feature.

## 4. Option I — integrate OneCLI as the host-side vault (recommended)

Keep the egress broker exactly as specified (no secret injection), and put the vault
*behind* it as a separate host-side principal:

```
sandbox (network=none)                      trusted host
  agent: http_fetch("vault://github/...")        │
        │  unix socket (egress broker, T-111)     │
        ▼                                          ▼
  egress broker ──── forwards by name ───▶ OneCLI vault (host-side)
  (no secrets, deny-by-default,            • holds the raw credential
   per-request audit — UNCHANGED)          • per-agent policy + rate limit
                                           • injects at request time
                                                  │ HTTPS, key attached host-side
                                                  ▼
                                           approved external API
```

The key move: **the secret never enters the sandbox *and* never enters our broker.**
The broker still forwards "the request's own bytes" — it just forwards them to the
OneCLI endpoint (a host-local address on the broker allowlist) instead of directly
to the API. OneCLI is the one component that holds keys and injects them, and it is
a tool built and audited for precisely that. B4-E stays true for *our* broker; the
injection boundary is OneCLI's, which is its designed purpose.

**Why this is the right call:**
- **Smallest change to our trust boundary.** The egress broker's specification and
  blast radius are unchanged. We add one allowlisted host-local destination (the
  vault) and one mapping layer — no new secret-handling code in the broker.
- **Reuse audited infrastructure.** OneCLI already implements per-agent policy,
  rate limits, and request-time injection. We do not reinvent a secret store or a
  policy engine — both are high-risk to build and a poor use of differentiation.
- **Defense in depth composes.** The agent is still in a `network=none` sandbox; the
  call still crosses the broker's deny-by-default allowlist and per-request audit;
  *then* the vault applies its own per-agent policy. Two independent gates, both
  host-side.
- **Clean non-goal preservation.** "A general arbitrary-API credential vault" stays
  a non-goal *for IronClaw to build* (SECURITY.md, threat-model §9). Integrating an
  external vault as an optional, operator-deployed component is a different posture
  from baking one into the trusted core.

## 5. Threat-model considerations for the integration

Hold these invariants (to be formalized in the threat-model addendum, T-260f):

- **No plaintext in the sandbox, ever.** The agent references a credential by
  logical name (`vault://<cred>/<path>`); it never receives the key. Verify OneCLI's
  response cannot echo the injected credential back through the broker (apply the
  T-107 redaction pattern on the broker→sandbox path as a backstop).
- **The vault is host-side and operator-deployed.** OneCLI runs on the trusted host
  (or a host-controlled service), reachable only as a broker-allowlisted
  destination — never a NIC inside the sandbox.
- **Audit joins both sides.** The broker already audits every request (host, path,
  method, status, bytes, duration); OneCLI audits injection + policy decisions. The
  integration must make these correlatable (a shared request id) so a credential use
  is traceable end-to-end — the §5 Repudiation control, extended.
- **Policy is host-owned and gateway-gated.** "Which group may use which credential
  against which host" is configuration; changing it must flow through the gateway
  like any other capability change, not be settable from the sandbox.

## 6. Open questions for the integration spike (must verify before T-260 build)

These are genuine unknowns a build task must close — flagged honestly rather than
assumed:

1. **OneCLI's actual injection API + self-hostability** — can it run fully host-side
   with no external dependency, and what is its request/response contract? (Drives
   the broker→vault mapping.)
2. **Authn between broker and vault** — how does OneCLI authenticate the *caller*
   (the broker, on behalf of a named agent group) so policy is enforced per group?
3. **Credential lifecycle** — provisioning, rotation, revocation: host-CLI only, or
   does it need a gateway change-kind?
4. **Licensing / provenance of OneCLI** — acceptable for IronClaw's supply-chain bar
   (cf. T-251..T-256)? If not, fall back to a *minimal* host-side injector behind the
   same architecture (Option B, but as a separate principal — never inside the
   broker).

## 7. Recommendation — INTEGRATE

Integrate OneCLI (or an equivalent purpose-built vault) as a **separate host-side
principal behind the egress broker**, and keep building-our-own-vault a non-goal.
This delivers request-time credential injection with per-agent policy and audit —
full parity with nanoclaw's headline secret story — while leaving IronClaw's two
most sensitive components (the sandbox seal and the minimal egress broker) exactly
as specified and audited.

Rejected: **build into the broker** (§3 — reverses B4-E, enlarges the trusted choke
point, reinvents an audited component for a P3 feature); **defer** (cedes a concrete
parity gap when a low-risk integration path exists).

## 8. Follow-up task breakdown (if 'integrate')

| Task | Scope | Owned paths (proposed) |
|---|---|---|
| **T-260a** OneCLI integration spike-2 | close the §6 open questions (API, authn, self-host, license); confirm the broker→vault contract | `.agents/spikes/credential-vault.md` (update) |
| **T-260b** Vault destination + naming | allowlist the host-local vault endpoint; define the `vault://<cred>/<path>` addressing the broker forwards | `internal/host/egress/**` |
| **T-260c** Per-group policy mapping (gateway-gated) | "group → credential → host" policy as gateway-approved config; no sandbox-settable policy | `internal/host/registry/**`, `cmd/controlplane` |
| **T-260d** Audit correlation | shared request id across broker + vault; end-to-end traceability of a credential use | `internal/host/egress/**` |
| **T-260e** Response redaction backstop | apply the T-107 redaction pattern on the broker→sandbox path so an injected credential can never echo back | `internal/host/egress/**` |
| **T-260f** Threat-model addendum + sign-off | document the vault-behind-broker boundary; keep "build our own" a non-goal | `docs/threat-model.md` |

None of these touch `internal/contract/**`. The egress-broker tasks (T-260b/d/e)
must be reviewed against the B4 invariants so the broker itself never becomes a
secret sink — the whole point of the integrate-don't-build decision.
