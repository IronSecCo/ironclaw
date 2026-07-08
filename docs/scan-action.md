# Scan in CI: a sandbox scorecard on every pull request

The [`ironctl scan`](scan.md) audit also ships as a **GitHub Action**, so every
repository can render an IronClaw containment scorecard right in its pull
requests and gate merges on isolation posture. It is the same local, read-only,
credential-free grader; the action just installs `ironctl`, runs it against your
target, and posts the result as a sticky PR comment.

## Add it to your workflow

```yaml
name: Sandbox scorecard
on: [pull_request]

permissions:
  contents: read
  pull-requests: write        # only needed for the sticky comment

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

On the pull request you get a scorecard comment that updates in place on every
push:

> ### IronClaw containment scan: `my-sandbox` scored **100/100 (grade A)**
>
> | Dimension | Verdict | Score |
> |---|---|---|
> | Non-root user (uid != 0) | :white_check_mark: PASS | 15/15 |
> | Dropped capabilities | :white_check_mark: PASS | 20/20 |
> | Seccomp profile | :white_check_mark: PASS | 15/15 |
> | Network isolation / egress | :white_check_mark: PASS | 15/15 |
> | Read-only root filesystem | :white_check_mark: PASS | 10/10 |
> | No docker.sock exposure | :white_check_mark: PASS | 15/15 |
> | No shared host namespaces | :white_check_mark: PASS | 10/10 |

## Grade a compose service or a k8s manifest

The file modes need no Docker daemon — the action reads the file directly:

```yaml
# a docker-compose service
- uses: IronSecCo/ironclaw/.github/actions/scan@v1
  with:
    mode: compose
    target: docker-compose.yml
    service: app            # required only if the file has >1 service

# a Kubernetes pod / workload manifest
- uses: IronSecCo/ironclaw/.github/actions/scan@v1
  with:
    mode: k8s
    target: deploy/pod.yaml
```

## Inputs

| Input | Default | Description |
|---|---|---|
| `target` | *(required)* | Container name/id, or path to a compose/k8s file. |
| `mode` | `container` | `container`, `compose`, or `k8s`. |
| `service` | `""` | Compose service name (when the file has more than one). |
| `min-score` | `0` | Fail the check below this score. `0` = report-only. |
| `comment` | `true` | Post the scorecard as a sticky PR comment. |
| `badge` | `false` | Write and upload the SVG badge as a build artifact. |
| `version` | `latest` | ironctl release (`latest` or a tag like `v0.1.252`). |
| `github-token` | `${{ github.token }}` | Token for the comment + release lookup. |

Outputs: `score`, `grade`, `scorecard` (path), `badge-path`, and `passed`.

## Report-only vs. gating

- Leave `min-score` at `0` (the default) to run **report-only**: the scorecard
  is posted and the check always passes. Good for adopting the action without
  risk, or watching a score trend before you enforce it.
- Set `min-score` (for example `90` for an A) to **gate**: the check fails when
  the posture regresses below the bar. The comment is still posted first, so
  reviewers see exactly which dimension slipped.

## How it works

The action downloads the released `ironctl` binary, runs `ironctl scan` against
your target, extracts the markdown scorecard, and posts it. The sticky comment
is keyed by mode + target, so re-runs update in place instead of spamming, and
multiple targets in one PR each keep their own comment. On non-PR events the
scorecard goes to the job summary instead.

Everything the [scan](scan.md) command guarantees still holds: it is local and
read-only, it never talks to a control-plane, it needs no credentials, and it is
fail-closed. See [`.github/actions/scan`](https://github.com/IronSecCo/ironclaw/tree/main/.github/actions/scan)
for the action source, and the [scan reference](scan.md) for the dimension
weights and grade bands.
