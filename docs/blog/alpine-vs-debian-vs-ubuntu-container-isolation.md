---
title: "Alpine vs Debian vs Ubuntu: does your base image change container isolation?"
description: "We scored the seven most-pulled base OS images (alpine, debian, ubuntu, busybox, amazonlinux, rockylinux, fedora) for container isolation with ironctl scan. Every one landed on the same 48 out of 100, grade D. Base image choice barely moves the isolation needle. Here is why, with the per-dimension data, and the docker run flags that actually take any of them to 100."
---

# Alpine vs Debian vs Ubuntu: does your base image change container isolation?

Search "alpine vs debian" or "is alpine more secure" and you get a hundred posts
arguing about package counts and CVE surface. Those are real questions. But they
answer a different question than the one most people are actually asking, which
is: **if I run untrusted code in this container, how much isolation stands between
that code and my host?**

That is a containment question, not a base-image question. So we measured it.
We ran [`ironctl scan`](audit-your-sandbox-in-10-seconds.md) over the seven
most-pulled base OS images in their default `docker run` configuration and
graded each 0 to 100 on how contained it is.

The result is the same number, seven times.

## The scores

| Base image | Isolation score | Grade |
| --- | ---: | :---: |
| `alpine:3.21` | 48 | D |
| `debian:12-slim` | 48 | D |
| `ubuntu:24.04` | 48 | D |
| `busybox:1.37` | 48 | D |
| `amazonlinux` | 48 | D |
| `rockylinux` | 48 | D |
| `fedora` | 48 | D |

Not close-together. Identical. And it is not just the total: the per-dimension
breakdown is byte-for-byte the same across all seven.

| Dimension | alpine | debian | ubuntu | Max |
| --- | :---: | :---: | :---: | ---: |
| Non-root user | 0 | 0 | 0 | 15 |
| Dropped capabilities | 4 | 4 | 4 | 20 |
| Seccomp profile | 15 | 15 | 15 | 15 |
| Network isolation / egress | 4 | 4 | 4 | 15 |
| Read-only root filesystem | 0 | 0 | 0 | 10 |
| No docker.sock exposure | 15 | 15 | 15 | 15 |
| No shared host namespaces | 10 | 10 | 10 | 10 |
| **Total** | **48** | **48** | **48** | **100** |

## Why they tie

Isolation is a property of *how you run the container*, not of *what is inside the
image*. `ironctl scan` grades the runtime posture the container gets from the host:
which capabilities it keeps, whether it runs as root, whether the root filesystem
is writable, whether egress is open. None of those are decided by whether the
userland was assembled from Alpine's apk or Debian's apt.

Every one of these images ships the same way by default: root user (uid 0), full
default capability set, writable root filesystem, open egress on the default
bridge. That is why they all land on 48. The base image you pick changes your CVE
surface and your image size. It does not, on its own, change your blast radius when
code inside the container turns hostile.

This does not mean Alpine and Debian are interchangeable. A smaller userland with
fewer packages means fewer things to patch, and that is a legitimate reason to
prefer a minimal base. But "fewer packages" is a supply-chain argument, not a
containment one. If you switch base images expecting a stronger boundary and change
nothing else, the boundary is exactly as strong as it was.

## What actually moves the number

The same handful of `docker run` flags takes every one of these images from 48 to
a clean 100 out of 100, grade A, without changing the base image at all:

```bash
docker run -d \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  alpine:3.21
```

Swap `alpine:3.21` for `debian:12-slim` or `ubuntu:24.04` and the score is still
100. The flags are what carry the grade, not the image. `ironctl scan --fix` writes
this stanza for you from any failing scan.

## The honest takeaway

If your question is "which base image is more isolated," the data says: none of
them, by default, and they are tied. Pick your base image for size, package
freshness, and CVE surface. Then earn your isolation separately, with runtime
flags, and verify it with a number instead of a vibe.

## See where your images land

Every image above has a per-dimension scorecard with the exact flags that close
each gap. Browse the full interactive ranking across 151 images, filter by
category, and grab a copy-paste score badge for your own repo at the
**[Container Isolation Scores explorer](https://nivardsec.com/scores)**. The
[leaderboard](../scores/leaderboard.md) refreshes weekly.

Scan the image you actually run in about ten seconds:

```bash
ironctl scan your-image:tag
```
