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

      - uses: IronSecCo/ironclaw@v1
        with:
          target: my-sandbox
          min-score: 90        # omit / 0 = report-only, never blocks the check
```

`IronSecCo/ironclaw@v1` is the [Marketplace](https://github.com/marketplace/actions/ironclaw-sandbox-scan)
listing (root `action.yml`). The subdir ref
`IronSecCo/ironclaw/.github/actions/scan@v1` runs the exact same grader and stays
supported as a fallback — both point at one `scan.sh`, so they never diverge.

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
- uses: IronSecCo/ironclaw@v1
  with:
    mode: compose
    target: docker-compose.yml
    service: app            # required only if the file has >1 service

# a Kubernetes pod / workload manifest
- uses: IronSecCo/ironclaw@v1
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
| `policy-check` | `false` | Policy-as-code gate (`mode=k8s` only): fail the check if the manifest violates the rules `--emit-policy` would generate. Needs no cluster or controller. Independent of `min-score`. |
| `comment` | `true` | Post the scorecard as a sticky PR comment. |
| `badge` | `false` | Write and upload the SVG badge as a build artifact. |
| `upload-sarif` | `false` | Emit SARIF and upload it to GitHub code scanning (Security tab). Requires `security-events: write`. |
| `version` | `latest` | ironctl release (`latest` or a tag like `v0.1.252`). |
| `github-token` | `${{ github.token }}` | Token for the comment + release lookup. |

Outputs: `score`, `grade`, `scorecard` (path), `badge-path`, `sarif-path`, `passed`, and `policy-passed`.

## Surface findings in the Security tab (SARIF)

Set `upload-sarif: true` and grant `security-events: write`, and the action
uploads a [SARIF 2.1.0](scan.md#github-code-scanning-security-tab) log so every
failed isolation dimension shows up in your repo's **Security > Code scanning**
tab, deduped across runs, alongside CodeQL and any other scanner:

```yaml
permissions:
  contents: read
  security-events: write      # required to upload SARIF
  pull-requests: write        # only if you also keep the sticky comment

jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: IronSecCo/ironclaw@v1
        with:
          mode: compose
          target: docker-compose.yml
          upload-sarif: true
```

`upload-sarif` defaults to `false`, so the action asks for **no** new permission
unless you opt in. Results are anchored at the scanned config file (with a line
region when derivable), and a clean 100/A target uploads zero findings. SARIF
emit is fail-open: a write failure never blocks the scan or its `min-score` gate.
The upload uses `github/codeql-action/upload-sarif`, pinned by commit SHA.

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

For the file modes (`compose` / `k8s`) the comment also shows a **delta versus
the base branch**: the action fetches the base version of the scanned file via
the contents API, grades it, and adds a line such as
`Δ vs base (main): +12 — base scored 88/100. Posture improved.` so a reviewer
sees at a glance whether the change hardened or regressed the posture. A file
that is new on the PR is noted as such, and the whole delta step is fail-open —
if the base cannot be fetched or graded, the scorecard is still posted without a
delta line. `container` mode has no git base to compare against, so it shows no
delta.

Every comment also ends with a copy-paste **README badge** snippet and an "Add
this to your README" nudge, rendered offline by `ironctl scan --badge-md`. Paste
it once and your repo carries a live containment-grade badge that links back to
the scan receipt, turning a one-off PR check into a persistent, self-promoting
signal. The snippet lives inside the same sticky comment, so it is idempotent and
never double-posted.

Everything the [scan](scan.md) command guarantees still holds: it is local and
read-only, it never talks to a control-plane, it needs no credentials, and it is
fail-closed. See [`.github/actions/scan`](https://github.com/IronSecCo/ironclaw/tree/main/.github/actions/scan)
for the action source, and the [scan reference](scan.md) for the dimension
weights and grade bands.
