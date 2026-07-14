---
title: "How to scan a Dockerfile for security issues (before and after: 5/100 to 100/100)"
description: "A Dockerfile security scanner you can run with no daemon and no image pull. A deliberately bad Dockerfile scores 5/100 (grade F): root default, unpinned base, a baked-in secret. The exact fixes that take it to 100/100 grade A."
---

# How to scan a Dockerfile for security issues

Most container security problems are written into the image before it ever runs. A `USER`
that defaults to root, a base image pinned to `:latest`, an API token baked into an `ENV`
layer: none of these need the container to start to be a problem, and none of them show up in
a runtime scan of the finished image because by then the damage is already in the layers.

`ironctl scan --dockerfile` grades that authoring-time posture statically. No daemon, no
image pull, no build. It reads the Dockerfile, grades seven independent dimensions on a 0 to
100 scale, and fails closed: anything it cannot confirm is safe is scored insecure. This
guide runs it on a deliberately bad Dockerfile, then on a hardened one, and shows the exact
gap between them, straight from the scan output.

> Honest ceiling up front: a static Dockerfile scan grades **authoring-time** posture only.
> Runtime hardening (dropped capabilities, seccomp, `network=none`, a read-only rootfs, no
> `docker.sock`) is set at `docker run` or orchestration time and cannot be expressed in a
> Dockerfile, so it is not graded here. A high static grade is necessary but not sufficient.
> You still want a runtime scan: `ironctl scan <container>`. More on that at the end.

## The Dockerfile that scores 5 out of 100

Here is a Node.js Dockerfile that looks unremarkable and is riddled with issues:

```dockerfile
FROM node:latest

ENV API_TOKEN=changeme-do-not-commit
ENV NODE_ENV=production

ADD https://example.com/app.tar.gz /app/

WORKDIR /app
COPY . .
RUN npm install
RUN chmod -R 777 /app

CMD ["node", "server.js"]
```

Scan it:

```bash
ironctl scan --dockerfile Dockerfile
```

```
IronClaw containment scan
  target:  Dockerfile (dockerfile)
  score:   5/100  grade F  (wide open)

DIMENSION                    VERDICT   SCORE  DETAIL
Non-root USER                [x] FAIL  0/25   no USER instruction: the image defaults to root (uid 0)
Pinned base image            [x] FAIL  0/20   base "node:latest" is unpinned (:latest / implicit): non-reproducible, silent drift
No secrets in ENV/ARG        [x] FAIL  0/20   secret-like literal(s) baked into ENV/ARG: API_TOKEN (persists in image layers / build history)
COPY over remote/opaque ADD  [x] FAIL  0/12   remote ADD fetches over the network into a layer (no checksum, MITM): https://example.com/app.tar.gz
No world-writable files      [x] FAIL  0/10   world-writable permissions granted: chmod -R 777 (any in-container user can tamper)
HEALTHCHECK defined          [~] WARN  0/8    no HEALTHCHECK: a hung process is invisible to the orchestrator
Layer / cache hygiene        [+] PASS  5/5    no unpruned package caches detected
```

Five of 100. The only points come from layer hygiene. Every posture that ships a compromise
into the image failed.

## What each failure actually costs you

The weights are not arbitrary. The heaviest sit on the dimensions that bake a compromise into
the image itself:

- **Non-root USER (0/25).** No `USER` instruction means the image runs as root (uid 0). This
  is the single most searched-for Dockerfile issue for a reason: if a process escapes the
  container, it escapes as root on the host. Every runtime capability drop you add later is
  fighting uphill against a root default.
- **Pinned base image (0/20).** `node:latest` is a moving target. The image you tested is not
  the image that ships next week. An unpinned base is silent supply-chain drift: you cannot
  reproduce a build, and you cannot audit what changed.
- **No secrets in ENV/ARG (0/20).** `ENV API_TOKEN=sk_live_...` writes the credential into an
  image layer. It persists in the layer history and in `docker history` forever, even if a
  later layer unsets it. Anyone who can pull the image has the token.
- **COPY over remote/opaque ADD (0/12).** `ADD https://...` fetches over the network into a
  layer with no checksum. A compromised or spoofed host, or a plain MITM, injects whatever it
  wants into your build.
- **No world-writable files (0/10).** `chmod -R 777 /app` lets any user inside the container
  rewrite the application code, including an attacker who lands as a low-privilege user.
- **HEALTHCHECK (0/8).** No liveness probe, so a wedged process is invisible to the
  orchestrator. Lower weight because it is reliability, not a direct breach.

## The hardened Dockerfile that scores 100

Fix every failing dimension. Multi-stage build, digest-pinned base, an explicit non-root
`USER`, no baked secret, `COPY` instead of remote `ADD`, no world-writable modes, a
`HEALTHCHECK`:

```dockerfile
# Pin the base to an immutable digest instead of a moving tag.
FROM node@sha256:<digest> AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY . .

FROM node@sha256:<digest>
WORKDIR /app
COPY --from=build --chown=65532:65532 /app /app
USER 65532:65532
HEALTHCHECK --interval=30s CMD node healthcheck.js
CMD ["node", "server.js"]
```

The secret is gone: inject `API_TOKEN` at runtime with `docker run -e API_TOKEN=...` or a
mounted secret, never in the Dockerfile. Rescan:

```bash
ironctl scan --dockerfile Dockerfile
```

```
IronClaw containment scan
  target:  Dockerfile (dockerfile)
  score:   100/100  grade A  (hardened)

DIMENSION                    VERDICT   SCORE  DETAIL
Non-root USER                [+] PASS  25/25  final stage runs as USER 65532:65532 (non-root)
Pinned base image            [+] PASS  20/20  base pinned to an immutable digest (node@sha256:2f3b7c8d9e0a…)
No secrets in ENV/ARG        [+] PASS  20/20  no secret-like literal values in ENV/ARG
COPY over remote/opaque ADD  [+] PASS  12/12  no remote or archive-extracting ADD (COPY used for local files)
No world-writable files      [+] PASS  10/10  no world-writable (chmod 777 / o+w) permissions
HEALTHCHECK defined          [+] PASS  8/8    HEALTHCHECK declared: orchestrators can detect a wedged container
Layer / cache hygiene        [+] PASS  5/5    no unpruned package caches detected
```

A 95-point swing, no runtime flags, no daemon, no image pull. Every fix is a one-line change
the scan named for you.

## Gate it in CI and on every commit

The point of a static scan is to catch these in review, not production. Two ways to enforce a
floor.

Fail a commit locally before it is ever pushed, with the [pre-commit](https://pre-commit.com)
hook. It builds `ironctl` from source, so there is nothing to install first:

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/IronSecCo/ironclaw
    rev: v0.1.x
    hooks:
      - id: ironclaw-scan-dockerfile
        args: [--min-score=80]   # fail the commit below grade B
```

Gate a pull request in CI with `--min-score`, and emit SARIF so failures show up inline in the
GitHub Code scanning tab:

```bash
ironctl scan --dockerfile Dockerfile --min-score 80    # non-zero exit below the floor
ironctl scan --dockerfile Dockerfile --sarif df.sarif  # upload to GitHub code scanning
```

## Scan your own Dockerfile in ten seconds

The grades above are one example. Yours is what matters:

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade a Dockerfile statically, no daemon needed
ironctl scan --dockerfile Dockerfile
```

Pass more than one path (`ironctl scan --dockerfile services/*/Dockerfile`) to grade an
entire repo at once.

## The runtime half you still need

Static grading closes the authoring-time holes. It cannot see how the container is actually
run, and that is where the other half of container security lives: dropped capabilities,
seccomp, `network=none`, a read-only rootfs, no `docker.sock`. A Dockerfile that scores 100
can still be started wide open. Grade the running container too:

```bash
ironctl scan my-container        # 0 to 100 runtime containment grade
ironctl scan my-container --fix  # the exact docker run flags to close each gap
```

- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference, static and runtime.
- [How to run untrusted Node.js code safely &rarr;](run-untrusted-nodejs-code-safely.md): the runtime companion to this guide.
- [Container Isolation Scores directory &rarr;](../scores/index.md): the default grade for 150+ of the most-pulled public images.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
