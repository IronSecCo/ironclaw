---
title: "Docker default vs hardened: the container isolation score gap, measured"
description: "How much isolation do you gain by hardening a docker run command? We scored 151 popular images on defaults (average 52 out of 100, grade D) and re-scored them hardened (100 out of 100, grade A, every single one). Here is the exact 48-point jump, dimension by dimension, and the six flags that produce it."
---

# Docker default vs hardened: the container isolation score gap, measured

"Harden your containers" is advice everyone repeats and almost nobody quantifies.
How much isolation does hardening actually buy you? We put a number on it.

We ran [`ironctl scan`](audit-your-sandbox-in-10-seconds.md) over 151 of the
most-pulled public Docker images twice: once on the exact `docker run` defaults
their own READMEs tell you to use, and once with a standard hardening stanza
applied. The scan grades seven containment dimensions 0 to 100.

The gap is not subtle.

| Configuration | Average score | Grade range |
| --- | ---: | :---: |
| Default `docker run` | 52 | D to C (48 to 63) |
| Hardened `docker run` | 100 | A (every image) |

On defaults, not one of the 151 images reaches an A. Scores run from 48 to 63.
Hardened, **all 151 land on a clean 100**. The full leaderboard on defaults is
[here](../scores/leaderboard.md); the delta below is what hardening adds on top.

## The six flags that carry the grade

Here is the hardened command the scan grades. It is base-image agnostic; it scores
100 whether the last argument is `alpine`, `debian`, `node`, or `postgres`:

```bash
docker run -d \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  your-image:tag
```

## Dimension by dimension: where the 48 points come from

Take a typical default-configured image at 48 out of 100 and watch each flag close
a specific hole:

| Dimension | Default | Hardened | Flag that closes it | Max |
| --- | :---: | :---: | --- | ---: |
| Non-root user | 0 | 15 | `--user 65532:65532` | 15 |
| Dropped capabilities | 4 | 20 | `--cap-drop=ALL` | 20 |
| Read-only root filesystem | 0 | 10 | `--read-only --tmpfs /tmp` | 10 |
| Network isolation / egress | 4 | 15 | `--network=none` | 15 |
| Seccomp profile | 15 | 15 | already on by default | 15 |
| No docker.sock exposure | 15 | 15 | do not mount the socket | 15 |
| No shared host namespaces | 10 | 10 | do not share host namespaces | 10 |
| **Total** | **48** | **100** | | **100** |

Four dimensions do the work. Docker's default seccomp profile already earns full
marks, and most images do not mount the docker socket or share host namespaces, so
those three are usually green before you touch anything. The points you are leaving
on the table are the first four: **root user, full capabilities, writable
filesystem, and open egress.**

## What separates a C from a D on defaults

The only reason any default image scores above 48 is that it already dropped one of
those habits. The images at the top of the leaderboard, at 63 out of 100, are the
ones whose image sets a non-root user in the Dockerfile. That single choice is worth
15 points:

| Dimension | `alpine` (48, D) | `adminer` (63, C) |
| --- | :---: | :---: |
| Non-root user | 0 | 15 |
| every other dimension | same | same |

That is the whole difference between the best and worst default scores in a
151-image survey: one dimension. Which is the point. Isolation on defaults is a
narrow band because almost every image makes the same four omissions.

## The catch hardening does not fix

Every flag above is real, worthwhile hardening, and it takes you from a D to an A on
the config scan. But the scan grades *configuration posture*, and there is one thing
configuration cannot change: a hardened container still shares the host kernel. When
we ran a live escape suite, hardening a container blocked 4 of 5 escape attempts, up
from 2 of 5 on raw defaults. The one it never blocks is the kernel-sharing attempt.
Closing that last gap needs a different runtime, not a different flag. That is a
separate decision, covered in
[gVisor vs runc](gvisor-vs-runc-container-isolation-compared.md).

For the config layer, though, the math is clean: six flags, +48 points, D to A, on
every image we tested.

## Scan and fix your own images

Every scorecard names the exact flags for that image, and `ironctl scan --fix`
writes the hardened stanza for you. Browse the full interactive ranking and grab a
score badge for your repo at the
**[Container Isolation Scores explorer](https://nivardsec.com/scores)**.

```bash
ironctl scan your-image:tag        # see your default score
ironctl scan your-image:tag --fix  # get the hardened command
```
