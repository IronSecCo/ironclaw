# IronClaw Launch-Week Engagement Playbook

How we show up, monitor, and respond during launch week and the weeks after. The goal is a
**community flywheel**: fast, credible, human responses convert curiosity into stars, stars
into contributors. This playbook makes that repeatable instead of improvised.

- **Owner:** Growth / DevRel (WS-E)
- **Gate:** launch execution is gated on **WS-G green + WS-H UX bar cleared + WS-A/B/C done**
  (IRO-40). This playbook is staged and ready; **do not post launch threads before the gate.**
- **Golden rule:** every claim we make maps to something shipped and verifiable. No vaporware.
  When in doubt, confirm capability with QA before publishing.

---

## 1. Where we post (channel map)

Sequenced, not simultaneous. Lead with the channels where a security/dev audience evaluates
substance, then amplify.

| Order | Channel | Format | Primary message angle | Copy source |
| --- | --- | --- | --- | --- |
| 1 | **Show HN** | Show HN post + first comment | "Secured, open-source alternative to openclaw/nanoclaw — gVisor isolation, approval gateway, signed supply chain." Lead with the demo. | launch-announcement doc (IRO-186) |
| 2 | **r/selfhosted** | text post | Self-host trust + one-command zero-cred demo | launch-announcement doc |
| 3 | **r/opensource / r/devops** | text post | OSS + supply-chain attestation story | launch-announcement doc |
| 4 | **Project blog** | long-form announcement | Full positioning + comparison + demo embed | docs/blog (IRO-118) |
| 5 | **X / Mastodon** | thread | Demo GIF + 5 differentiators, 1 per post | launch-announcement doc |
| 6 | **LinkedIn** | single post | Trust/compliance framing for orgs | launch-announcement doc |
| 7 | **GitHub Discussions → Announcements** | pinned post | "We launched" + how to get involved | this repo |

Channel-fit reminder: **HN ≠ Reddit ≠ LinkedIn.** HN wants substance and a working demo and
punishes marketing tone. Reddit wants a self-hoster talking to self-hosters. LinkedIn tolerates
the trust/compliance angle. Reuse the *claims*, rewrite the *voice* per channel.

## 2. Monitoring cadence (launch day → week)

| Window | Cadence | What to watch |
| --- | --- | --- |
| Launch day, first 4h | every **15–20 min** | HN comments, Reddit comments, new issues, Discussions Q&A |
| Launch day, rest | every **45–60 min** | same + X/Mastodon replies, new stars velocity |
| Days 2–7 | **2–3×/day** | issues, Discussions, PR queue, AWL/directory mentions |
| Steady state | **1×/day** + weekly metrics | issues, Discussions, contributor PRs |

Response-time goal: **first reply within 30 min** during the launch-day windows; same business
day thereafter. On HN especially, a fast, substantive author reply to the top critical comment
is worth more than the post itself.

Surfaces to keep open: HN thread · Reddit thread(s) · `github.com/IronSecCo/ironclaw/issues` ·
`/discussions` · X/Mastodon notifications · `scripts/community/adoption-snapshot.sh` for live numbers.

## 3. Response templates

Adapt tone per surface; keep substance constant. Never paste secrets or internal infra details.
Every security claim must trace to shipped, verifiable capability (cite docs/threat-model.md,
SBOM/attestation, or a QA report).

### 3a. "How is this different from openclaw/nanoclaw?"
> IronClaw is the **security-hardened** path. Same in-session agent UX, but every run is isolated
> in a gVisor sandbox, credentials sit behind a deny-by-default approval gateway (the agent
> proposes, a human approves), queues are encrypted at rest, and the supply chain is signed +
> attested (cosign, SBOM, SLSA provenance, reproducible builds). Full comparison: [link to
> docs/comparison.md]. If trust/isolation isn't a concern for you, the upstreams are great — we
> exist for the people who need to prove what ran.

### 3b. "Is this actually secure or is 'secured' just marketing?"
> Fair question — for a security tool, claims should be checkable. Concretely: gVisor
> (runsc) isolation [threat-model.md], approval gateway with deny-by-default credential vault,
> encrypted SQLCipher queues, and a signed/attested release (`cosign verify` works on the
> published image and binaries; SBOM + SLSA provenance attached). Here's the verification
> walkthrough: [link]. Found a gap? That's exactly what we want — open an issue or email security.

### 3c. "Show me it working"
> 30-second, zero-credential demo: `[one-command demo]` — it stands up the control plane, an
> isolated sandbox, and a mock agent so you can watch the propose → approve → execute loop with
> nothing to sign up for. Walkthrough: [link]. The animated demo is on the README and landing page.

### 3d. New issue (bug)
> Thanks for the report — and for trying IronClaw this early. To get this triaged fast: what OS +
> version, what command, and the output of `ironctl doctor`? If it's a sandbox issue, the early-exit
> diagnostics in the logs (`ic-sbx-<id>`) help a lot. Tracking this now.

### 3e. New issue / Discussion (feature idea)
> Love this — added to Ideas. Want to scope it together? If you're up for a PR, I can point you at
> the right files and we'll tag it good-first-issue. Roadmap context: [ROADMAP.md].

### 3f. First-time contributor PR
> Welcome, and thank you — first-time contributor 🎉. CI will run the standard checks; the
> 5-minute contributor quickstart is in CONTRIBUTING.md. I'll review within a day. Don't worry
> about getting it perfect — we'll iterate together.

### 3g. Skeptical / negative comment
> Appreciate the pushback — keeps us honest. [Address the specific point with a fact + link.] If we
> got something wrong, I genuinely want to fix it; here's where to file it: [link].

> Never argue, never overclaim, never get defensive. Concede real limitations openly — for a
> security audience, admitting a boundary builds more trust than spinning it.

## 4. Pre-launch checklist (Growth-owned slice)

- [ ] Launch gate (IRO-40) confirmed green by CEO/board — **hard dependency**
- [ ] Every claim in launch copy re-verified against shipped capability (QA sign-off cited)
- [ ] Baseline metrics snapshot captured the morning of launch (`adoption-snapshot.sh`)
- [ ] Demo verified working on a clean machine that day
- [ ] Discussions categories seeded; Announcements post drafted & ready to pin
- [ ] Response templates loaded; monitoring surfaces bookmarked
- [ ] Comparison page, threat-model, and verification walkthrough links live and correct
- [ ] Team coverage agreed for the first-4-hours window

## 5. After launch

- Capture a metrics snapshot at **24h, 72h, and 1 week**; record reads in adoption-metrics.md.
- Collect social proof (notable stars, quotes, mentions) → file a child issue to curate a wall.
- Feed every recurring onboarding-friction question back to Forge (WS-D) and UXDesigner (WS-H).
- Post a "thank you + what's next" Announcements thread at the end of week one.

## Related

- Adoption metrics: [adoption-metrics.md](adoption-metrics.md)
- Launch announcement copy: IRO-186 (`launch-announcement` doc)
- Launch kit / claim-tagged copy: IRO-99 (`launch-kit` doc)
- Comparison / positioning: docs/comparison.md
- Contributor on-ramp: ROADMAP.md, CONTRIBUTING.md (IRO-209)
