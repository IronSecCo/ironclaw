---
title: "Add a live Sandbox Isolation Score badge to your repo"
description: "A coverage badge tells the world your tests pass. A Sandbox Isolation Score badge tells them your container holds. Generate one with ironctl scan --badge-json, commit the JSON as a static file, and shields.io renders a live 0 to 100 A-to-F grade in your README. No server, no scan on every badge hit."
---

# Add a live Sandbox Isolation Score badge to your repo

Your README already advertises what you care about. A coverage badge says the
tests pass. A build badge says the pipeline is green. A signed-release badge says
the artifacts are attested. Each one is a small, public, always-current claim that
a stranger can trust at a glance.

If you run untrusted code in a container, or ship a project that tells other people
to, there is one claim missing: how much containment do you actually have? A
Sandbox Isolation Score badge answers that in the same glanceable format, backed by
a real audit of your container config instead of an adjective.

![Sandbox Isolation](https://img.shields.io/badge/sandbox%20isolation-100%2F100%20A-3fb950)

This post shows how to generate that badge with one command, host it as a plain
committed file, and embed it. It takes about two minutes and stands up zero
infrastructure.

## What the score means

The badge shows a number from 0 to 100 and a letter grade from A to F. That number
is not a vibe. `ironctl scan` reads your container's runtime configuration and
grades seven independent containment dimensions, then sums them:

| Dimension | Weight | What it checks |
|---|---|---|
| Non-root user | 15 | the process runs as a uid other than 0 |
| Dropped capabilities | 20 | Linux capabilities are dropped, not the default set |
| Seccomp profile | 15 | a seccomp filter narrows the syscall surface |
| Network isolation | 15 | no bridge egress; prefer `network=none` |
| Read-only root filesystem | 10 | the rootfs cannot be written for tamper or persistence |
| No docker.sock exposure | 15 | the Docker socket is not mounted in |
| No shared host namespaces | 10 | PID, network, and IPC namespaces are not shared with the host |

A wide-open container started the way a lot of quickstart guides suggest (root
user, default capabilities, bridge network, `docker.sock` mounted for convenience)
scores 23 out of 100, grade F. A hardened sandbox that drops every capability, runs
non-root, mounts a read-only rootfs, and takes no network scores 100 out of 100,
grade A. The scan is fail-closed: any dimension it cannot observe is scored as
insecure, never waved through. Full detail on each dimension is in the
[scan reference](../scan.md).

## Generate the badge JSON

One command grades a target and writes a
[shields.io endpoint](https://shields.io/badges/endpoint-badge) JSON file:

```bash
# grade a running container and write the badge file
ironctl scan my-container --badge-json .ironclaw/sandbox-isolation.json
```

The same flag works on the other scan targets:

```bash
# a docker-compose service
ironctl scan --compose docker-compose.yml --service app --badge-json .ironclaw/sandbox-isolation.json

# a Kubernetes pod manifest
ironctl scan --k8s pod.yaml --badge-json .ironclaw/sandbox-isolation.json
```

The written file is tiny and fully self-contained:

```json
{
  "schemaVersion": 1,
  "label": "sandbox isolation",
  "message": "100/100 A",
  "color": "3fb950"
}
```

That is the entire endpoint contract shields.io expects. The score and color are
pinned at generation time. Grade to color follows the scorecard palette: A is
green, B and C are amber, D and F are red.

## Host it as a committed file

Here is the design decision that matters: you commit that JSON to your repository
and let shields.io read it as raw content. You do not stand up a service.

```bash
git add .ironclaw/sandbox-isolation.json
git commit -m "chore: add sandbox isolation badge"
```

A live scanning endpoint would mean every badge render triggers a scan of a remote
target, which is a denial-of-service surface pointed at your own infrastructure and
a slow, flaky badge for readers. A committed file has none of that. It renders
instantly from a CDN, it cannot be knocked over, and it says exactly what your
posture was the last time you graded it. When your container config changes, you
rerun the one-liner, commit the refreshed file, and the badge follows along.

## Embed it

Point a shields.io endpoint badge at the raw file, swapping in your
`OWNER/REPO/BRANCH/PATH`:

```markdown
[![Sandbox Isolation Score](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/OWNER/REPO/main/.ironclaw/sandbox-isolation.json)](https://ironsecco.github.io/ironclaw/scan/)
```

If you would rather not commit a file at all, a plain
[static badge](https://shields.io/badges/static-badge) with the grade baked into
the URL works too, at the cost of updating it by hand when your posture changes:

```markdown
![Sandbox Isolation](https://img.shields.io/badge/sandbox%20isolation-100%2F100%20A-3fb950)
```

## Keep it honest with a CI gate

A badge that drifts out of date is worse than no badge, because it makes a stale
claim look current. Regenerate the file in CI and add a floor so a posture
regression fails the build instead of shipping a green badge that lies:

```bash
# fail the build if the sandbox drops below an A, then refresh the badge
ironctl scan my-container --min-score 90
ironctl scan my-container --badge-json .ironclaw/sandbox-isolation.json
```

Now the badge is a checked fact on every push, not a screenshot from six months
ago.

## We dogfood this

IronClaw carries its own Sandbox Isolation Score badge in the project README,
graded 100 out of 100, A. It is generated from
[`.ironclaw/sandbox-posture.yml`](https://github.com/IronSecCo/ironclaw/blob/main/.ironclaw/sandbox-posture.yml),
the reference posture the isolation launcher applies to every per-session sandbox:
distroless non-root image, all capabilities dropped, no new privileges, read-only
rootfs, seccomp confined, and `network=none`. The badge is not a marketing graphic;
it is the output of running our own audit tool against our own default, committed
to the repo like everyone else's.

## Add yours

```bash
brew install ironsecco/ironclaw/ironclaw
ironctl scan <your-container> --badge-json .ironclaw/sandbox-isolation.json
```

Commit the file, embed the snippet, and your README now makes a containment claim a
stranger can trust at a glance. For every flag and dimension in detail, see the
[scan reference](../scan.md). To see how IronClaw builds a 100 out of 100 posture
for every agent session, see the reproducible
[containment benchmark](containment-benchmark-docker-gvisor-e2b-daytona.md).
