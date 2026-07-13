---
title: "IronClaw scan is now a GitHub Action on the Marketplace"
description: "The ironctl scan containment grader ships as a GitHub Action on the Marketplace. Add uses: IronSecCo/ironclaw@v1 to any workflow and every pull request gets a 0 to 100 sandbox isolation scorecard as a sticky comment. Local, read-only, credential-free."
---

# IronClaw scan is now a GitHub Action on the Marketplace

The [`ironctl scan`](../scan.md) containment grader is now a
[GitHub Action on the Marketplace](https://github.com/marketplace/actions/ironclaw-sandbox-scan).
Add one line to a workflow and every pull request in your repository gets an
isolation scorecard, graded 0 to 100, posted as a sticky comment that updates in
place on each push. No new service, no account, no credentials.

It is the same grader that ships in the CLI. The Action just downloads the
released `ironctl` binary, runs `ironctl scan` against your target, and posts the
result. Everything the scan command guarantees still holds: it is local and
read-only, it never talks to a control-plane, and it is fail-closed, so any
dimension it cannot observe is graded insecure rather than waved through.

## One copy-paste to add it

```yaml
name: Sandbox scorecard
on: [pull_request]

permissions:
  contents: read
  pull-requests: write        # only for the sticky scorecard comment

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

`IronSecCo/ironclaw@v1` is the Marketplace listing, backed by the repository's
root `action.yml`. It is a thin wrapper around the same `scan.sh` that already
shipped under `.github/actions/scan`, so the older subdir ref
`IronSecCo/ironclaw/.github/actions/scan@v1` keeps working and the two never
diverge.

## What lands on the pull request

The Action posts a scorecard comment keyed by mode and target, so re-runs update
in place instead of spamming, and multiple targets each keep their own comment.
A clean IronClaw sandbox looks like this:

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

A container started the usual way, running as root with the default capability
set, a writable root filesystem, `docker.sock` mounted in, and bridge egress,
grades far lower. Every failing line names the specific hole and why it matters,
so the fix is obvious from the comment.

## What the score means

The number is not a vibe. `ironctl scan` reads your container's runtime
configuration and grades seven independent containment dimensions, then sums
them to 100:

| Dimension | Weight | What it checks |
|---|---|---|
| Non-root user | 15 | the process runs as a uid other than 0 |
| Dropped capabilities | 20 | Linux capabilities are dropped, not the default set |
| Seccomp profile | 15 | a seccomp filter narrows the syscall surface |
| Network isolation | 15 | no bridge egress; prefer `network=none` |
| Read-only root filesystem | 10 | the rootfs cannot be written for tamper or persistence |
| No docker.sock exposure | 15 | the Docker socket is not mounted in |
| No shared host namespaces | 10 | PID, network, and IPC namespaces are not shared with the host |

Grades map to bands: A is 90 and up, B is 75 to 89, C is 60 to 74, D is 40 to
59, F is below 40. See the [scan reference](../scan.md) for the exact weights and
every flag.

## Beyond the container mode

The same Action grades a compose service or a Kubernetes manifest with no Docker
daemon, because those modes read the file directly:

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

Leave `min-score` at `0` to run report-only while you watch a score trend, then
set it, for example `90` for an A, to gate merges once you are ready to enforce
the bar. You can also opt into `upload-sarif: true` to surface every failed
dimension in your repository's **Security > Code scanning** tab alongside CodeQL.

## Try it

- Full setup, all inputs, and outputs: [scan in CI](../scan-action.md).
- Grade a container locally first: [audit your sandbox in 10 seconds](audit-your-sandbox-in-10-seconds.md).
- Show the grade in your README: [add a Sandbox Isolation Score badge](add-a-sandbox-isolation-score-badge-to-your-repo.md).

Add `uses: IronSecCo/ironclaw@v1` to a workflow and let the first pull request
tell you how much containment your sandboxes actually have.
