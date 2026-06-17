# IronClaw Pitch — Q&A Prep

The ten questions most likely to come up after this deck, judged by what the deck leaves open, what
technical judges and VCs usually press on, and what is genuinely interesting about this market. Each
has a short answer you can say out loud and a one-liner if you are tight on time. Stay honest about
what is built versus roadmap; the audit story only works if we do not overclaim.

Ground rules for the room:
- Built and runnable today: gVisor sandbox (`network=none`), encrypted per-session SQLite queues,
  sealed Go binary, the capability gateway, 6 channel adapters, CLI/API-first, ~200 tests, MIT.
- Roadmap, say so plainly: enterprise governance (SSO, RBAC, policy-as-code, audit export), the
  GitHub-issue-to-patch developer workflow, the mesh-only web console.

---

## 1. It is MIT open source. How do you make money?

**Why they ask:** the deck says open source and MIT twice, then talks about a $52B market. They want
the bridge from free code to revenue.

**Answer:** Open core. The community edition is free and self-hosted forever; that is what wins the
developer and earns trust, because security you cannot inspect is not security. We charge for the
things a company needs but a solo developer does not: governance (SSO, RBAC, policy-as-code, audit
export), multi-tenant deployment, a managed/hosted runtime, and support. It is the GitLab and
HashiCorp pattern, and it fits here because the same architecture that wins the individual is exactly
what a security team has to sign off on. The free layer drives adoption; the control and compliance
layer is the budget line.

**One-liner:** Free for the developer, paid for the security team. Open core, with governance and
hosting as the commercial layer.

## 2. What is your moat? Why can't OpenClaw, or a big cloud, just add sandboxing and bury you?

**Why they ask:** the code is MIT, the incumbents have 100x your mindshare, and "secure mode" sounds
like a feature, not a company.

**Answer:** Three things. First, this is an architecture, not a feature flag. OpenClaw is built around
a single trusted operator; retrofitting a default-deny, host-mediated, sealed-runtime trust boundary
means changing the foundation, not toggling a setting. We started from that boundary. Second, the moat
is not the code, it is the credibility: being the substrate a security team has already reviewed and
approved, plus the governance and policy layer and the operational know-how to run it. Third, we are
complementary, not a rival: Claw-style agents can run inside our control plane, so the more those
ecosystems grow, the more surface we secure. The defensibility compounds on trust and enterprise
deployment, which are slow to copy.

**One-liner:** Secure-by-default is an architecture you commit to on day one, not a feature you bolt
on later, and our moat is review credibility plus the governance layer, not the source.

## 3. The gateway only gates the agent's own capability changes. So what actually protects me from the agent misusing the access it already has?

**Why they ask:** this is the sharp technical question, and the honest answer is what earns trust with
judges. Slide 5 is explicit that the agent acts freely within its grant.

**Answer:** Correct, and we are deliberate about it. We do two things. We shrink the blast radius:
`network=none` so there is nowhere to exfiltrate, repo-scoped mounts so it only sees what you gave it,
per-session isolation so one task cannot touch another, and host-brokered short-lived credentials so
secrets never live in the agent. And we make every action attributable and replayable in a log the
agent cannot edit. What we do not yet claim is to judge the semantic correctness of every in-scope
action; that is what the policy and risky-action-approval layer is for, and policy-as-code is on the
roadmap. We would rather state the boundary precisely than oversell it.

**One-liner:** We minimize what the agent can reach and make everything it does auditable; judging the
intent of each in-scope action is the policy layer, and we say so honestly.

## 4. You say "trust sits outside the agent." But the host holds the keys and writes the log. What is your real trust boundary, and what if the host is compromised?

**Why they ask:** the audit-trap argument is strong, so a sharp listener pushes it one level up: who
audits the auditor?

**Answer:** Our trusted root is the host control-plane, and the whole design assumes everything below
it, the agent and the sandbox box, can be hostile at any moment. That is the point: we move trust off
the large, dynamic, model-driven surface and onto a small, sealed, boring one. The host is a compiled
Go binary with no public web surface to phish, admin only over a private mesh, and it performs every
privileged action itself after its own checks. If the host is fully compromised, yes, it is game over;
but that is true of any system's trusted root, and ours is a far smaller and more hardened target than
"trust the agent." We are honest that we shrink the trusted computing base, we do not pretend to
eliminate it.

**One-liner:** We move trust to a small, sealed, mesh-only host instead of a sprawling agent; we
shrink the trusted base, we do not claim to magic it away.

## 5. What does gVisor plus per-session encrypted SQLite cost you in performance, and where does it actually run?

**Why they ask:** technical judges know gVisor adds syscall overhead and is Linux-only; they want to
know it is practical, not just principled.

**Answer:** Agent workloads are dominated by model latency and IO, not by syscall throughput, so the
gVisor overhead is in the noise for this use case; we are paying it exactly where it buys the most
isolation. Per-session SQLite is cheap and gives us encryption at rest and clean isolation for free.
On deployment: the sealed sandbox runs on Linux, which is where you host this anyway, and there is a
dev mode that runs locally in-memory without gVisor so developers get the five-minute experience on
any machine. So the security tax lands in production, where it matters, and not on the developer
trying it out.

**One-liner:** The workload is model-bound, so gVisor's cost is negligible here; it runs on Linux in
production, with an in-memory dev mode for instant local use.

## 6. Model support is Anthropic through a host proxy today. What about other providers, or local models?

**Why they ask:** lock-in and flexibility; also it probes whether the architecture is rigid.

**Answer:** Every model call from the sandbox goes out through a host-side proxy over a local socket,
because the sandbox has no network of its own. That proxy is provider-agnostic by design; today it
fronts Anthropic, and adding OpenAI-compatible or local model routing is a host-side change, not an
architecture change. We kept it single-provider for the 48 hours on purpose, but the seam is already
there. The important property is that the credential lives on the host and is injected per call, so
the agent never holds it regardless of which model is behind the proxy.

**One-liner:** The host proxy is provider-agnostic; we ship Anthropic today, and more providers are a
host-side add, with the credential always staying on the host.

## 7. This is 48 hours old. Do you have any users or design partners, or just the incumbents' star counts?

**Why they ask:** they are testing whether you will overclaim. The honest answer scores points.

**Answer:** Honestly, it is 48 hours old and just went open source, so we have no paid users yet; what
we have is a runnable product with around two hundred tests green and a clear, narrow security claim.
The evidence of demand is the category itself, the adoption numbers and the incumbents' traction, plus
our own enterprise conversations where trust is the blocker every time. That is exactly why the ask is
design partners and people willing to clone it and try to break it, not a revenue chart we do not have
yet. We would rather show you something real and small than a forecast.

**One-liner:** None yet, it shipped 48 hours ago; the proof is that it runs and the ask is design
partners, not invented traction.

## 8. Your market numbers are "research snapshots," and Gartner warns many agentic projects get cancelled. How real is this?

**Why they ask:** the deck labels its own figures as unverified, and the skeptic wants to know the
market is not hype.

**Answer:** Two parts. On the numbers, we are deliberately transparent that the headline figures are
directional snapshots we verify before any live use; the one thing that is not a snapshot is
willingness to pay, which Cursor and Copilot already prove at scale. On the cancellation risk, that
data actually argues for us: agentic projects stall on trust, governance, and security as much as on
capability, and that is the exact gap we close. We are not betting that every agent project succeeds;
we are betting that the ones that do will need a control plane, and the ones that fail often fail for
the reason we fix.

**One-liner:** The willingness to pay is already proven by Cursor and Copilot, and the projects that
fail often fail on the trust gap we are built to close.

## 9. You came as mentors and you already run an enterprise company. Is IronClaw a real company or a side project?

**Why they ask:** the founder story is charming but raises a focus-and-commitment flag, and a possible
conflict with your existing enterprise product.

**Answer:** It is the same mission from the other end. Our company builds secure AI agent
infrastructure for enterprises; IronClaw is the open, developer-facing front of that thesis, the
bottom-up wedge into the same market we already sell to top-down. The 48-hour origin is how it
started, not the scope of the commitment. There is no conflict: the enterprise work is the commercial
layer this open core feeds, and the two reinforce each other. If anything, having real enterprise
customers is why we know the trust gap is the blocker and not a guess.

**One-liner:** Same mission, two ends: IronClaw is the open developer wedge into the secure-agent
market our enterprise company already serves.

## 10. What is actually built versus roadmap, and how far is the enterprise story and the issue-to-patch workflow you showed?

**Why they ask:** the deck shows a live gateway demo and also a "next step" patch flow plus enterprise
governance; they want the line between real and aspirational.

**Answer:** Built and runnable today: the gVisor sandbox with no network, encrypted per-session
queues, the sealed runtime, the capability gateway, six channel adapters, CLI and API, one-command
install, and around two hundred tests. Roadmap, and we mark it as roadmap on the slides: the
GitHub-issue-to-patch developer workflow, which sits on top of the same gateway and sandbox we already
have, and the enterprise governance layer, SSO, RBAC, policy-as-code, audit export, plus a mesh-only
web console. The security foundation is the hard part and it is done; what is left is workflow and
packaging on top of it. We will not show you a demo of something that is not real.

**One-liner:** The security foundation is built and runs; the patch workflow and enterprise governance
are the roadmap on top of it, and we label them as such.

---

## Rapid-fire (shorter, still likely)

- **Pricing?** Not finalized. Free community edition; enterprise priced per seat in the range the
  market already pays for dev tooling ($20 to $40 a seat is the anchor, not a quote). Hosted runtime
  priced on usage.
- **Has it had a real security audit?** Not yet, and we will not pretend otherwise; "passes a security
  review" today means the design is built to survive one. A third-party audit is exactly what early
  funding and design partners would fund.
- **Why Go and gVisor instead of Docker like NanoClaw?** A compiled binary gives us the sealed runtime
  (no source for the agent to rewrite), and gVisor is a stronger isolation boundary than a stock
  container. Docker-level isolation depends heavily on how you deploy it; we wanted the boundary to be
  a property of the system, not of the operator's config.
- **Why the name / the Claw ecosystem?** We are the iron around the claw: the same agent power, inside
  a hardened shell. We position with the ecosystem, not against it.
- **Can I run my existing OpenClaw-style agent in it?** That is the goal of the control plane: bring
  the agent, inherit the sandbox, the keys, and the gateway. The adapter surface is how we get there.
- **What is the single biggest risk to this becoming a company?** Distribution, not technology. The
  security architecture is real; the race is becoming the default secure substrate before "secure
  mode" becomes table stakes elsewhere. That is why we are open source and why the ask is adoption.
