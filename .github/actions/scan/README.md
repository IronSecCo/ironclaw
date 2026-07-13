# IronClaw sandbox scan — GitHub Action

Grade the isolation posture of a container, a `docker-compose` service, or a
Kubernetes manifest **0-100** on every pull request, post the scorecard as a
sticky PR comment, and (optionally) fail the check when the score regresses.

It wraps [`ironctl scan`](https://ironsecco.github.io/ironclaw/scan/): a local,
read-only audit that inspects configuration (`docker inspect` / a compose or k8s
file) and **never talks to any control-plane or needs credentials**. The only
network access is downloading the released `ironctl` binary. Fail-closed: any
dimension it cannot determine is graded as insecure.

> **Marketplace shortcut:** the same action is published at the repo root, so you
> can also write the shorter `uses: IronSecCo/ironclaw@v1`. Both refs run one
> `scan.sh` and never diverge; this subdir path stays supported as a fallback.

## Quick start

```yaml
name: Sandbox scorecard
on: [pull_request]
permissions:
  contents: read
  pull-requests: write        # needed only for the sticky comment
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Start the container you want to grade
        run: docker run -d --name my-sandbox my-image:latest sleep 3600
      - uses: IronSecCo/ironclaw/.github/actions/scan@v1
        with:
          target: my-sandbox
          min-score: 90        # omit / 0 = report-only, never blocks the check
```

That renders a scorecard like:

> ### IronClaw containment scan: `my-sandbox` scored **100/100 (grade A)**
>
> | Dimension | Verdict | Score |
> |---|---|---|
> | Non-root user (uid != 0) | ✅ PASS | 15/15 |
> | Dropped capabilities | ✅ PASS | 20/20 |
> | … | … | … |

## Modes

Grade a **container**, a **compose** service, or a **k8s** manifest:

```yaml
# compose service (no Docker daemon needed — it reads the file)
- uses: IronSecCo/ironclaw/.github/actions/scan@v1
  with:
    mode: compose
    target: docker-compose.yml
    service: app            # required only if the file has >1 service

# kubernetes pod / workload manifest
- uses: IronSecCo/ironclaw/.github/actions/scan@v1
  with:
    mode: k8s
    target: deploy/pod.yaml
```

## Inputs

| Input | Default | Description |
|---|---|---|
| `target` | *(required)* | Container name/id, or path to a compose/k8s file. |
| `mode` | `container` | `container` \| `compose` \| `k8s`. |
| `service` | `""` | Compose service name (when the file has more than one). |
| `min-score` | `0` | Fail the check below this score. `0` = report-only. |
| `comment` | `true` | Post the scorecard as a sticky PR comment. |
| `badge` | `false` | Write and upload the SVG badge as a build artifact. |
| `upload-sarif` | `false` | Emit SARIF and upload to GitHub code scanning (Security tab). Requires `security-events: write`. |
| `version` | `latest` | ironctl release to use (`latest` or a tag like `v0.1.252`). |
| `github-token` | `${{ github.token }}` | Token for the comment + release lookup. |

## Outputs

| Output | Description |
|---|---|
| `score` | Overall containment score (0-100). |
| `grade` | Letter grade (A-F). |
| `scorecard` | Path to the rendered markdown scorecard. |
| `badge-path` | Path to the SVG badge (when `badge: true`). |
| `sarif-path` | Path to the SARIF log (when `upload-sarif: true`). |
| `passed` | `true` when the score met `min-score`. |

## What it grades

Seven boundaries whose breach is a full host compromise — non-root user, dropped
capabilities, seccomp, `network=none`, read-only rootfs, no `docker.sock`
exposure, and no shared host namespaces. See the
[scan docs](https://ironsecco.github.io/ironclaw/scan/) for the weights and the
grade bands.

## Notes

- The sticky comment updates in place (keyed by mode+target), so re-runs never
  spam the PR. Multiple targets in one PR each get their own comment.
- On non-PR events (push, dispatch) the scorecard is written to the job summary
  instead of a comment.
- Grant `pull-requests: write` if you want the comment; without it the action
  still runs and gates, it just skips commenting.
