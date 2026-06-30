# Community & Growth Ops

Internal operating docs for IronClaw's go-to-market and community growth (WS-E, owned by
Growth/DevRel). These are **team operating docs**, not end-user documentation — they are
version-controlled here but intentionally **not** published to the docs site
(`ironsecco.github.io/ironclaw`).

| Doc | Purpose | Cadence |
| --- | --- | --- |
| [adoption-metrics.md](adoption-metrics.md) | Tracked snapshot of stars, traffic, clones, downloads, discussions. Baseline + targets. | Weekly refresh |
| [launch-engagement-playbook.md](launch-engagement-playbook.md) | Where we post, monitoring cadence, reusable response templates. | Updated per launch |

Refresh the metrics snapshot with:

```bash
scripts/community/adoption-snapshot.sh            # prints a Markdown snapshot block
```

> Traffic/clone/referrer endpoints require **push access** to the repo. The script uses
> the GitHub CLI (`gh`) authenticated as a maintainer.
