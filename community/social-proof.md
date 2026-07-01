# Social-proof curation (internal)

Internal operating doc for how we collect and vet social proof, and what is allowed onto the
public wall at [docs/social-proof.md](../docs/social-proof.md). This is a **team operating
doc**, not published to the docs site. The public wall is the published artifact; this file
is the collection process, the sourcing rules, and the raw-capture ledger behind it.

- **Owner:** Growth / DevRel (WS-E)
- **Feeds:** the public wall (`docs/social-proof.md`), future launch copy, and the
  [launch-engagement-playbook](launch-engagement-playbook.md) flywheel (section 5).
- **Cadence:** capture snapshots at **24h, 72h, and 1 week** post launch (mirrors the
  playbook monitoring windows), then weekly in steady state.

## Sourcing guardrails (hard rules)

These are non negotiable and match the launch identity guardrails on IRO-271 / IRO-40.

1. **Permitted sources only.** Collect mentions and quotes from the channels we actually
   posted to or that are neutral public dev spaces:
   - Project blog
   - Pseudonymous Show HN post and its thread
   - Owner Reddit posts (r/selfhosted, r/opensource, r/devops) and their threads
   - Neutral dev channels: HN, Reddit, GitHub Discussions, newsletters, directory and
     awesome-list listings
2. **No personal-identity scraping.** Do **not** scrape, quote, or attribute anything via the
   owner's personal GitHub, LinkedIn, or Instagram. The owner's personal accounts are off
   limits as sources.
3. **Stars stay org-level.** Cite star and fork **counts** and named **orgs** that have made
   their use public. Do not build a wall of individual stargazer handles.
4. **Public and verbatim.** Only quote what was posted publicly. Keep quotes exact, link the
   source, and record the date. Never paraphrase a private DM or email as a public quote.
5. **Opt-out honored.** If someone asks to be removed, remove them promptly. Note it below.
6. **No vaporware.** A quote praising a capability only goes up if that capability is shipped
   and verifiable (playbook golden rule).

## Promotion rule (raw capture to public wall)

A candidate moves from the ledger below to `docs/social-proof.md` only when it is:
(a) from a permitted source, (b) public with a working link, (c) verbatim, and
(d) not covered by an opt-out. Anything failing a check stays here or gets dropped.

## Snapshot log

| Date | Stars | Forks | Notes |
| --- | --- | --- | --- |
| 2026-07-02 (launch) | 13 | 2 | Baseline captured at launch. Refresh with `scripts/community/adoption-snapshot.sh`. |

## Raw-capture ledger

Drop candidates here as they come in, before vetting. Move vetted ones to the public wall.

### Quote candidates

_None yet. Add as: source link, handle/first name, channel, date, verbatim text, vetted? (y/n)._

### Mention candidates

_None yet. Add as: publication/list, link, date, one-line summary, vetted? (y/n)._

### Opt-outs

_None._

## Related

- Public wall: [docs/social-proof.md](../docs/social-proof.md)
- Launch playbook (section 5, "After launch"): [launch-engagement-playbook.md](launch-engagement-playbook.md)
- Adoption metrics: [adoption-metrics.md](adoption-metrics.md)
- Amplification queue (newsletter / directory submissions): [amplification-submissions-queue.md](amplification-submissions-queue.md)
