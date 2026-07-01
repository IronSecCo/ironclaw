# IronClaw Developer-Community Amplification: Submissions Queue

Ready-to-fire submissions for developer newsletters and curated lists, each angled on the
**provable isolation** story. This queue is **staged, not fired**. Drafting is non-gated;
**submission is gated on IRO-40** (launch sign-off: WS-G green + WS-H UX bar cleared +
WS-A/B/C done). The goal is same-day distribution the moment the board clears launch, instead
of writing copy from scratch under time pressure.

- **Owner:** Growth / DevRel (WS-E)
- **Consistency:** copy here mirrors the launch-announcement positioning (IRO-186) and the
  content pipeline (IRO-213). One-line positioning: *Security-first, self-hosted AI agents:
  isolation you can prove, not just promise.*
- **House style:** no em or en dashes in any public copy (standing company rule, IRO-254).
  Every claim below is tagged to a shipped, verifiable feature.
- **Verification:** before firing, re-confirm each tagged claim against `main` with QA
  (per the launch-engagement-playbook pre-launch checklist). Do not fire a claim QA has not
  re-verified on launch day.

---

## The wedge: why "provable isolation" is the angle

For a developer and security audience, "secured" is a claim, not a feature. Our differentiator
is that the isolation boundary is **reproducible and checkable by the reader**, not asserted:

- **Red-team escape harness you can run yourself.** `examples/red-team-escape/` stands up a
  sandboxed agent and runs escape attempts; the core containment assertions pass. Shipped in
  PR #266 (IRO-257). This is the headline proof point: readers reproduce it, they do not take
  our word.
- **`network=none` per sandbox by default.** Each sandboxed agent runs with no network path;
  egress is deny-by-default. Documented in the README security model and
  `docs/site/concepts/sandbox-isolation.mdx`.
- **gVisor (runsc) syscall isolation.** The sandbox runs under a user-space kernel, not just a
  container namespace. WS-G capability gate verified (IRO-84).
- **Signed and attested releases.** `cosign verify` works on the published image and binaries;
  SBOM (SPDX + CycloneDX) and SLSA build provenance are attached; builds are reproducible.
  OpenSSF Scorecard and OpenSSF Best Practices badges are live on the README.

Every submission below leads with the first bullet (reproduce the escape tests) because
"show, do not tell" is strongest for this audience.

---

## Submission entries

Character limits below are conservative working targets based on each outlet's typical format.
**Re-check the live form limit at submission time**; if an outlet allows more, the shorter copy
still works. Counts are for the body/description field unless noted.

### 1. Console.dev (developer tools newsletter)

- **What it is:** curated newsletter and directory of developer tools; strong fit for a
  self-hosted security tool.
- **URL / process:** submit at `https://console.dev/submit-a-tool/`. Fields: tool name, URL,
  short tagline, longer description, category (choose Security or DevOps).
- **Format and limit:** tagline about 90 chars; description about 300 chars.

**Tagline (about 85 chars):**
> Self-hosted AI agents in a sandbox you can prove holds, with a red-team harness you run.

**Description (about 290 chars):**
> IronClaw runs autonomous AI agents on your own infrastructure. Each agent is sandboxed with
> network=none by default and gVisor syscall isolation. You do not have to trust the boundary:
> `examples/red-team-escape/` runs escape attempts and shows containment holding. Releases are
> cosign-signed with SBOM and SLSA provenance.

- **Claim tags:** sandbox + network=none (README security model); gVisor/runsc (IRO-84);
  red-team harness (PR #266, IRO-257); cosign + SBOM + SLSA (README release verification).
- **Fire when:** IRO-40 sign-off. No dependency beyond the gate.

### 2. Changelog News

- **What it is:** open-source and developer news, curated from community submissions; feeds
  the Changelog newsletter and site.
- **URL / process:** submit a link at `https://changelog.com/news/submit`. Fields: URL,
  headline, short comment on why it matters. Point the URL at the launch blog post (IRO-118),
  not the bare repo, so the story leads.
- **Format and limit:** headline about 80 chars; comment about 280 chars.

**Headline (about 70 chars):**
> IronClaw: self-hosted AI agents with isolation you can reproduce

**Comment (about 270 chars):**
> An AGPLv3 (plus commercial) alternative to hosted agent platforms. Agents run sandboxed with
> network=none and gVisor isolation, and the repo ships a red-team escape harness so you can
> reproduce the containment tests yourself. Releases are signed with SBOM and SLSA provenance.

- **Claim tags:** license (LICENSING.md); sandbox + network=none (README); gVisor (IRO-84);
  red-team harness (PR #266); signed releases (README).
- **Fire when:** IRO-40 sign-off, and launch blog post published (IRO-118) so the link target
  exists.

### 3. Golang Weekly

- **What it is:** weekly Go newsletter (Cooperpress), large Go-developer reach. IronClaw is a
  Go project, so lead with the Go angle plus the security hook.
- **URL / process:** suggest a link via the "mention it to us" form at
  `https://golangweekly.com/` (footer submission link), or email the Cooperpress editors.
  Fields: URL, one-line reason.
- **Format and limit:** one to two sentences, about 220 chars.

**Blurb (about 210 chars):**
> IronClaw is a Go runtime for self-hosted AI agents that treats the agent as untrusted: each
> runs sandboxed with network=none and gVisor isolation. The repo ships a red-team escape
> harness so you can reproduce the containment tests.

- **Claim tags:** Go project (repo language / build state); sandbox + network=none (README);
  gVisor (IRO-84); red-team harness (PR #266).
- **Fire when:** IRO-40 sign-off. Time submission to hit the next weekly issue.

### 4. TLDR (target: TLDR InfoSec)

- **What it is:** high-volume daily newsletter with topic editions. **TLDR InfoSec** is the
  best-fit edition for a security runtime; TLDR Programming is a fallback.
- **URL / process:** submit via the link form at `https://tldr.tech/` (Submit a link), or
  email the InfoSec editors. TLDR copy is extremely short and factual, no marketing tone.
- **Format and limit:** one sentence, about 180 chars, plus URL.

**Blurb (about 170 chars):**
> IronClaw is an open-source, self-hosted runtime for AI agents that sandboxes each agent with
> network=none and gVisor, and ships a red-team harness to reproduce the escape tests.

- **Claim tags:** open source (LICENSING.md); sandbox + network=none (README); gVisor
  (IRO-84); red-team harness (PR #266).
- **Fire when:** IRO-40 sign-off. Keep tone flat and factual; TLDR strips hype.

### 5. Hacker Newsletter

- **What it is:** weekly curation of the best Hacker News posts. It curates **from** HN, so the
  real lever is a strong Show HN; a good thread is likely to be picked up, and a direct
  suggestion reinforces it.
- **URL / process:** the Show HN post is the primary path (see launch-engagement-playbook
  channel map, order 1). Optionally suggest the thread at
  `https://hackernewsletter.com/` (submit / suggest link) once the Show HN is live.
- **Format and limit:** suggest the HN thread URL plus a one-line note, about 150 chars.

**Suggestion note (about 140 chars):**
> Show HN: IronClaw, self-hosted AI agents with a sandbox you can prove holds via a red-team
> escape harness you run yourself.

- **Claim tags:** Show HN copy is owned by the launch-announcement doc (IRO-186); all claims
  inherit that doc's QA-verified tags.
- **Fire when:** IRO-40 sign-off **and** the Show HN post is live and gaining traction. This
  entry is downstream of the Show HN, not independent.

### 6. awesome-selfhosted and awesome-go refresh

- **What it is:** two high-traffic curated GitHub lists. Inclusion is durable, evergreen
  discovery, not a one-day spike.
- **URL / process:** open a PR to each list following its exact entry format and alphabetical
  placement.
  - `awesome-selfhosted`: add under the relevant category (Automation / Self-hosting or
    AI/LLM, per the list's current taxonomy). Note: this list has a submission window; prior
    tracking (IRO-98) deferred the PR to about October. Confirm the window is open before
    filing.
  - `awesome-go`: `https://github.com/avelino/awesome-go`, add under the relevant category
    (for example Bot Building or a security/agents category if present). Requires the repo to
    meet the list's quality bar (tests, docs, license), which IronClaw already meets.

**awesome-selfhosted entry (one line, list format):**
> - [IronClaw](https://github.com/IronSecCo/ironclaw) - Self-hosted runtime for autonomous AI
>   agents, sandboxed with network=none and gVisor isolation, with a reproducible red-team
>   escape harness and signed, attested releases. `AGPL-3.0` `Docker/Go`

**awesome-go entry (one line, list format):**
> - [IronClaw](https://github.com/IronSecCo/ironclaw) - Security-first, self-hosted runtime for
>   autonomous AI agents with gVisor sandbox isolation and a reproducible red-team escape harness.

- **Claim tags:** sandbox + network=none (README); gVisor (IRO-84); red-team harness
  (PR #266); signed/attested releases (README); license (LICENSING.md).
- **Fire when:** IRO-40 sign-off for awesome-go (file the PR on launch day). For
  awesome-selfhosted, gate is IRO-40 **and** the list's submission window being open (track via
  IRO-98).

---

## Fire-order and coordination

1. **On IRO-40 green:** confirm QA has re-verified every tagged claim on `main` that day.
2. **Same-day, in order:** Show HN first (owned by launch-announcement / playbook), then
   Console.dev, Golang Weekly, TLDR InfoSec, Changelog News. These are independent and can go
   the same day.
3. **After Show HN has traction:** suggest the thread to Hacker Newsletter.
4. **awesome-go PR:** file on launch day. **awesome-selfhosted PR:** file when the submission
   window is open (IRO-98).
5. Record each submission (date, outlet, status) so we do not double-submit and can attribute
   referral traffic in adoption-metrics.md.

## Related

- Launch channel map and response templates: [launch-engagement-playbook.md](launch-engagement-playbook.md)
- Launch announcement copy: IRO-186 (`launch-announcement` doc)
- Content pipeline / calendar: IRO-213
- Directory and list discoverability tracking: IRO-98, IRO-177
- Positioning and comparison: docs/comparison.md
- Adoption metrics: adoption-metrics.md
