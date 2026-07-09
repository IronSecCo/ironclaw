---
title: "State of Container Isolation 2026: we graded 16 popular images and 15 scored a D or worse"
description: "We ran ironctl scan over 16 popular public container images in their common run configurations and graded each on a 0 to 100 containment scale. Only one hit an A. Thirteen landed on a D. The median default image scores 48 of 100, running as root with the full capability set and a writable root filesystem. Here is the data, the method, and how to re-run it yourself."
---

# State of Container Isolation 2026

If you copy a `docker run` line out of a quickstart and hand it untrusted code,
how much isolation are you actually getting? Most teams cannot answer with a
number. We decided to measure it.

We took 16 popular public images in the configurations people actually run them
in, graded each one with [`ironctl scan`](audit-your-sandbox-in-10-seconds.md)
on a 0 to 100 containment scale, and looked at the distribution. The short
version: the default posture of the container ecosystem in 2026 is weak, and it
is weak in the same three ways almost every time.

Everything below is reproducible. The dataset, the pinned image manifest, and
the one-command harness live in
[`examples/isolation-survey`](https://github.com/IronSecCo/ironclaw/tree/main/examples/isolation-survey).
No credentials, no cloud, no account. If you disagree with a number, re-run it.

## The headline

Sixteen scenarios. Here is where they landed.

| Grade | Scenarios | What it means |
|:-----:|:---------:|---------------|
| A (>=90) | 1 | Explicitly hardened. Almost nothing to hand the host. |
| C (>=70) | 1 | One good default, several gaps. |
| D (>=50) | 13 | The plain-`docker run` baseline. Root, full caps, writable rootfs. |
| F (<50)  | 1 | A dangerous CI or ops pattern layered on top. |

Fifteen of sixteen scored below 70. Thirteen of those are not misconfigured or
exotic. They are the images you already run, started exactly the way their own
README tells you to.

## The default is a D

Strip away the naive CI patterns and look only at the `default-*` family:
twelve popular images (nginx, postgres, redis, mysql, mongo, node, python,
golang, httpd, rabbitmq, wordpress, memcached) each started with a plain
`docker run` and zero hardening flags.

Eleven of those twelve score **48 of 100**. Not a spread. The same score, over
and over, because they all fail the same three dimensions:

- **Non-root user.** They run as uid 0. A container escape starts with host uid 0.
- **Dropped capabilities.** They keep the full default Linux capability set.
- **Read-only root filesystem.** The root fs is writable, giving tamper and
  persistence surface.

The scanner grades seven dimensions, weighted by how much of the host each one
hands over when it fails. The default image passes the cheap ones (no
`docker.sock` mounted, no shared host namespaces) and fails the ones that
actually matter for a breakout. That is why the number clusters so tightly at
48: the default configuration is a single, shared posture, and it is a D.

The one image that breaks the pattern is **memcached**, at 63 of 100 (a C). The
only difference is that its image drops to a non-root user by default. That
single decision, made once by the image maintainer, is the entire gap between a
D and a C. It is not expensive. It is just not the default anywhere else.

## The naive patterns fall off a cliff

The `naive-*` family takes a popular base image and layers on a common but
dangerous operational pattern. These are not strawmen. Every one is something
people do in real CI and ops setups:

| Scenario | Pattern | Score |
|----------|---------|------:|
| `naive-ci-docker-sock` | bind-mount `docker.sock` to "build images in CI" | 33/100 (D) |
| `naive-host-ns` | share host namespaces for "a monitoring sidecar" | 34/100 (D) |
| `naive-privileged` | `--privileged` for docker-in-docker | 19/100 (F) |

A bind-mounted `docker.sock` is root on the host by another name: anything in
the container can create a new container that mounts the host filesystem.
`--privileged` disables seccomp, grants every capability, and drops the
namespace walls in one flag. The scores reflect that. Convenience flags are not
free; they are the difference between a D and an F.

## Hardening is six flags and it is free

The `hardened-reference` scenario is the same `nginx:1.27-alpine` image as one
of the D-grade defaults. It scores **100 of 100, an A**. The only difference is
the run command:

```bash
docker run \
  --user 65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --read-only --tmpfs /tmp \
  --network none \
  nginx:1.27-alpine
```

Six flags. No new tooling, no runtime swap, no rebuild. The same image that
scores 48 as a copy-pasted `docker run` scores 100 when you close the gaps the
scanner points at. The knowledge, not the capability, is what is missing across
the ecosystem.

## The method, so you can argue with it

Every number above comes from `ironctl scan`, which reads a workload's runtime
configuration and grades seven containment dimensions:

| Dimension | Weight | Fails when |
|-----------|:------:|------------|
| Dropped capabilities | 20 | the default Linux capability set is retained |
| Non-root user | 15 | the container runs as uid 0 |
| Seccomp profile | 15 | the syscall filter is disabled |
| Network isolation | 15 | egress is possible (anything but `--network none`) |
| No docker.sock exposure | 15 | the host Docker or OCI socket is bind-mounted |
| Read-only root filesystem | 10 | the root fs is writable |
| No shared host namespaces | 10 | `--pid host`, `--network host`, or `--ipc host` |

Grading is **fail-closed**: any dimension the scanner cannot determine is scored
as insecure, never silently passed. Grades map A (>=90) down to F (<50). Every
image in the survey is pinned by its multi-arch manifest-list digest, so a
`docker pull` resolves byte-identical bits on amd64 and arm64. The survey was
generated on 2026-07-08 with `ironctl` at dev.

Want the per-image breakdown? See the
[Container Isolation Scores directory](../scores/index.md) for one scorecard page
per image, each with its full dimension-by-dimension grade and the exact flags
that close the gap.

Re-run the whole thing from a clean checkout with nothing but a Docker daemon:

```bash
git clone https://github.com/IronSecCo/ironclaw
examples/isolation-survey/survey.sh
# writes results.json and results.md
```

## What to take away

1. **The default posture of a container is a D.** If you have not passed
   hardening flags, assume you are running as root with the full capability set
   and a writable filesystem. Measure it; do not guess.
2. **The gap between a D and an A is a run command, not a rewrite.** Six flags
   move the same image from 48 to 100. Start with `--user`, `--cap-drop ALL`,
   and `--read-only`.
3. **Convenience flags are the cliff.** A bind-mounted `docker.sock` or
   `--privileged` turns a weak container into an open door to the host.

If you want the number for your own setup, point the scanner at it:

```bash
ironctl scan my-container
```

It runs on your own containers, compose services, and Kubernetes pods. You do
not need to adopt IronClaw to use it. And if you want the isolation your
agents actually need by default, that is
[what IronClaw is for](https://github.com/IronSecCo/ironclaw).
