# Directory / OSS-tracker listings (IRO-306)

Non-launch-gated organic-discovery listings, split out from the IRO-40-gated
announcement queue ([amplification-submissions-queue.md](amplification-submissions-queue.md)).
Follow-up from the IRO-277 SEO audit directory target list ([seo-audit.md](seo-audit.md)).

- **Owner:** Growth / DevRel (WS-E)
- **House style:** no em or en dashes in any public copy (standing rule, IRO-254).
- **Identity guardrails (IRO-271):** no personal GitHub / LinkedIn / Instagram, no
  owner name in any listing. Company identity only.
- **No launch claim:** these listings point at the repo and describe shipped
  capability. They do not announce a launch and are not gated on IRO-40.

## Verification result (2026-07-03): the "no-account" premise does not hold

The original target list assumed these were repo-URL-based listings that "need no
owner-manual post and make no launch claim." On checking the live submission flows,
that is only partly true. Findings:

| Target | Live state (checked 2026-07-03) | Can Growth file it unattended? |
| --- | --- | --- |
| **OpenSourceAgenda** | **Defunct.** `opensourceagenda.com` now 301-redirects to an unrelated real-estate site. The OSS tracker no longer exists. | No. Removed from the target list. Do not submit to a dead domain. |
| **AlternativeTo** | Live, but submitting a new app requires a **registered account and login** (crowd-sourced, moderated). A listing under the "IronClaw" name already appears to exist and describes a *different* project (Rust, Apache-2.0, ~12k stars, "nearai") that is **not** our Go / AGPLv3 / IronSecCo project. Name collision to resolve before any submit. | No. Needs a company account (external account creation, CEO approval) and manual identity handling; plus collision triage. |
| **LibHunt** | Live. **Passive by design**: auto-indexes GitHub repos and monitors HN / Reddit / Dev.to mentions in near real time. Manual "Add a project" also exists but requires an account. | Partly. No action needed to be indexed; it populates from our (already accurate, IRO-177) GitHub topics and from post-launch mentions. Manual add still needs an account. |
| **awesome-\* lists (5 PRs, IRO-98)** | PRs live on external list repos, opened from the owner account. | No. Owner-manual; Growth lacks visibility into external-repo PRs opened under a personal identity. Monitoring is the owner's path. |

**Conclusion:** none of the remaining targets is a true "paste a repo URL, no
account" submission. Each needs either a company account (CEO approval per the
Growth safety contract on external account creation) or a personal GitHub identity
(forbidden by IRO-271). The organic-discovery win that requires **no** account,
LibHunt indexing, is already covered by the accurate GitHub topics shipped in
IRO-177. This is escalated to the CEO (see below); the copy is staged fire-ready so
that once an account path is approved the submit is a paste, not a writing task.

## Fire-ready packet: AlternativeTo

Use only after the CEO approves an account path (see escalation). Resolve the
name-collision entry first: search AlternativeTo for "IronClaw"; if the existing
entry is a different project, submit ours under a disambiguated name
(for example "IronClaw (IronSecCo)") or request a correction, do not overwrite an
unrelated project.

- **Submit flow:** create/sign in to an account, then "Add application".
- **Name:** IronClaw
- **Homepage URL:** https://ironsecco.github.io/ironclaw
- **Source / repo URL:** https://github.com/IronSecCo/ironclaw
- **License:** Open Source, AGPL-3.0 (with a commercial option)
- **Platforms:** Self-Hosted, Linux, Mac, Windows (via WSL2), Docker
- **Categories / tags:** Developer Tools, AI Agents, Self-Hosted, Security,
  AI Coding Assistant. (Not "AI Chatbot", which is what the colliding entry uses.)
- **Listed as an alternative to:** openclaw, nanoclaw, hosted AI agent platforms.

**Tagline (about 90 chars):**
> Self-hosted AI agents in a sandbox you can prove holds, with a red-team harness you run.

**Description (about 280 chars):**
> IronClaw runs autonomous AI agents on your own infrastructure. Each agent is
> sandboxed with network=none by default and gVisor syscall isolation, and the repo
> ships a red-team escape harness so you can reproduce the containment tests yourself.
> Releases are cosign-signed with SBOM and SLSA provenance. AGPL-3.0.

- **Claim tags (verify on `main` with QA before submit):** sandbox + network=none
  (README security model); gVisor / runsc (IRO-84); red-team harness
  (PR #266, IRO-257); cosign + SBOM + SLSA (README release verification);
  license (LICENSING.md).

## Escalation to CEO

Filing any of the remaining targets requires an external company account
(AlternativeTo; LibHunt manual add) which, per the Growth safety contract, needs
CEO approval before creation, and an identity decision that stays within the
IRO-271 guardrails (no owner personal identity). Growth cannot create a
company-facing external account unilaterally. Decision requested: whether to stand
up a company account for these directories and, if so, under what identity, or to
rely on passive LibHunt indexing plus the owner-manual awesome-\* PRs and defer
active directory submissions. Copy above is staged so approval converts to a
same-session paste-and-submit.

## Related

- SEO / discoverability audit and target list: [seo-audit.md](seo-audit.md) (IRO-277)
- Launch-gated announcement queue: [amplification-submissions-queue.md](amplification-submissions-queue.md) (IRO-269)
- Repo metadata / GitHub topics (live): IRO-177
- awesome-\* list PRs: IRO-98
- Identity guardrails: IRO-271. No-em-dash rule: IRO-254.
