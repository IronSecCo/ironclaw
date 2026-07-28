# Parked content backlog (board decision on IRO-606)

Single index of the written-but-unposted go-to-market content. Everything listed
here is finished copy that was never published because the channel needs an
account or identity the board declined to create. **The work is parked, not
lost.** This file is the durable pointer so a cancelled issue thread never
becomes the only record of it.

- **Owner:** Growth / DevRel (WS-E)
- **Decision of record:** board answer on IRO-606, interaction `96a6b54d`,
  answers `g3_cancel`, `g4_cancel`, `dir_nongated`.
- **Parked on:** 2026-07-29 (IRO-608).

## Why it is parked

- **Gate 4, posting identity: `g4_cancel`. We stay repo-only.** No
  project-branded Reddit, X, or AlternativeTo identity will be created, and
  IRO-265 still forbids the owner's real name and personal GitHub, LinkedIn, or
  Instagram accounts. With no company identity and no personal identity, there
  is no legal channel for external posting. Every Reddit, Show HN, and
  newsletter draft below is therefore unpublishable today, regardless of
  quality.
- **Gate 3, MCP directories: `g3_cancel`.** Smithery, mcp.so, and
  mcpservers.org all require a registered account behind a web form. The board
  declined to create them. See the correction below on what this does and does
  not cover.
- **Standing direction: `dir_nongated`.** Remaining capacity goes to non-gated
  product and OSS-community levers (IRO-475), not to writing more copy for
  channels we cannot post to.

Do not write more copy for these channels. The backlog is already deeper than
any future posting window will consume. If the board ever approves an identity,
the unparking task is a review-and-refresh pass, not a writing task.

## The parked assets

### Launch and social copy (channel-gated on an identity)

| Asset | Path | Channel | Unparks when |
| --- | --- | --- | --- |
| Show HN title + first comment | [`docs/launch/show-hn.md`](../docs/launch/show-hn.md) | Hacker News | An approved posting identity exists |
| r/devops self-post | [`docs/launch/reddit-devops.md`](../docs/launch/reddit-devops.md) | Reddit | Same |
| r/kubernetes self-post | [`docs/launch/reddit-kubernetes.md`](../docs/launch/reddit-kubernetes.md) | Reddit | Same |
| r/LocalLLaMA self-post | [`docs/launch/reddit-localllama.md`](../docs/launch/reddit-localllama.md) | Reddit | Same |
| r/programming link + comment | [`docs/launch/reddit-programming.md`](../docs/launch/reddit-programming.md) | Reddit | Same |
| r/selfhosted self-post | [`docs/launch/reddit-selfhosted.md`](../docs/launch/reddit-selfhosted.md) | Reddit | Same |
| Publishing guardrails + reusable hooks | [`docs/launch/README.md`](../docs/launch/README.md) | All | Read this first when unparking |
| Newsletter and curated-list submissions | [`amplification-submissions-queue.md`](amplification-submissions-queue.md) | Newsletters | Same |
| Framework-community posts (Discord, forums) | [`integration-framework-community-posts.md`](integration-framework-community-posts.md) | Framework communities | Same |
| Integration-guide launch posts | [`integration-guides-launch-posts.md`](integration-guides-launch-posts.md) | Social | Same |
| Launch-week engagement and response templates | [`launch-engagement-playbook.md`](launch-engagement-playbook.md) | All | Same |
| Directory and OSS-tracker listing copy | [`directory-listings.md`](directory-listings.md) | AlternativeTo, LibHunt | An approved account path |

`docs/launch/` is excluded from the published docs site
(`exclude_docs` in `mkdocs.yml`), so these drafts are version-controlled but not
served. `community/` is outside `docs/` entirely and is never built.

### Long-form posts (already published, not parked)

Two of the assets named in the IRO-608 brief turned out to be shipped docs-site
pages rather than unposted drafts. They need no rescue:

| Asset | Path | State |
| --- | --- | --- |
| gVisor deep-dive | [`docs/gvisor-deep-dive.md`](../docs/gvisor-deep-dive.md) | Live on the docs site |
| Bring-your-own-model | [`docs/bring-your-own-model.md`](../docs/bring-your-own-model.md) | Live on the docs site |
| Sandbox red-team proof piece | [`security-proof-writeup.md`](security-proof-writeup.md) | Drafted, needs sign-off, no channel |
| Containment proof pack | [`containment-proof-pack.md`](containment-proof-pack.md) | Data asset, non-gated |

### MCP Registry listing

The machine-readable listing is the tracked [`server.json`](../server.json) at
the repo root plus the runbook at
[`runbooks/mcp-registry.md`](../runbooks/mcp-registry.md). It is a build input,
not a draft, so it stays where the workflow reads it.

The human-facing directory listing copy (the prose blurbs written for Smithery,
mcp.so, and mcpservers.org) is parked at `content/drafts/mcp-directory-listing.md`
via PR #579, filed when IRO-378 was cancelled. See also the correction below:
the official registry listing itself is currently dark.

## Correction: the MCP Registry listing is not healthy

The `g3_cancel` rationale states that we are "already live on the official MCP
Registry as `io.github.IronSecCo/ironclaw` with OIDC auto-publish on every
release." Checked against `main` and against the live registry on 2026-07-29,
that is not the current state:

- **The listing is `deprecated`, not active.** The live registry record for
  `io.github.IronSecCo/ironclaw` reports `status: deprecated` at version
  `0.1.230`, published 2026-07-07.
- **It is stale by roughly 150 releases.** Current shipping version is around
  `0.1.383`.
- **It points at the rejected image.** The published package is
  `ghcr.io/ironsecco/ironclaw-controlplane:v0.1.230` with a
  `/var/run/docker.sock` mount, which is exactly the option-A listing the CEO
  rejected as host-root and against our hardening promise (IRO-391, IRO-414).
  It was deprecated on purpose via `.github/workflows/mcp-registry-deprecate.yml`.
- **Auto-publish is off.** The `publish-mcp-registry` job in
  `.github/workflows/image.yml` is hard-gated to a manual `workflow_dispatch`
  with `publish_mcp_registry=true` and never runs on the release path.
- **The replacement image does not exist yet.** `container/mcp.Dockerfile`
  defines the slim, socket-free `ironclaw-mcp` (option B, IRO-414), but it is
  referenced by **no workflow**: `image.yml` builds only
  `container/controlplane.Dockerfile`. `ghcr.io/ironsecco/ironclaw-mcp` is not
  anonymously pullable, while `ironclaw-controlplane` is. So the Dockerfile is
  in-tree but has never been built or published.

This does not change the cancellation. The board's decision was about not
creating accounts, and none of Smithery, mcp.so, or mcpservers.org becomes
reachable because of this. But the premise that the listing "that matters" is
already handled does not hold, and repairing it is a **non-gated, no-account**
lever: the registry authenticates the `io.github.IronSecCo` namespace with this
repo's own GitHub Actions OIDC token, so no human account is involved.

Note the sequencing, because it is easy to underestimate. This is not a
one-line repoint. The socket-free image has to be **built and published first**,
then `server.json` repointed at it, then the publish job re-enabled, then the
listing flipped back to `active`.

That is release-pipeline and image work, not Growth's. It is already owned:
**IRO-611** (Relay), "Relight the official MCP Registry listing." The parked
MCP directory listing copy is preserved separately at `content/drafts/`
(PR #579).

## What is not parked

The non-gated levers stay open and are where capacity goes under `dir_nongated`:

- `ironctl scan` and the public web scanner, the only wedge that needs no account.
- Docs-site SEO and the hardening-guide waves.
- OSS-community triage on our own repo, including the good-first-issue queue.
  Note: the open GitHub issues on the repo are deliberate newcomer bait. Do not
  mass-close them.
- The MCP Registry repair above, which is account-free.
