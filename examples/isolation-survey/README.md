# isolation-survey — a reproducible State of Container Isolation dataset

This example runs [`ironctl scan`](../../docs/scan.md) over a curated set of
popular **public** container images and their common run configurations, and
emits a combined machine-readable [`results.json`](./results.json) plus a
rendered [`results.md`](./results.md) table (image -> score -> grade -> top
failed dimensions).

It is the reproducible harness behind the "State of Container Isolation"
writeup: a defensible, repeatable measurement that anyone can rerun from a clean
checkout with nothing but Docker. No credentials, no cloud, no account.

```bash
# from the repo root, with a Docker daemon running:
examples/isolation-survey/survey.sh
# -> writes examples/isolation-survey/results.json and results.md
```

## What it measures

`ironctl scan` grades a workload's containment posture 0-100 across seven
dimensions, each weighted by how much of the host it hands over when it fails:

| Dimension | Weight | Fails when |
|-----------|:------:|------------|
| Dropped capabilities | 20 | the default Linux capability set is retained |
| Non-root user | 15 | the container runs as uid 0 |
| Seccomp profile | 15 | the syscall filter is disabled (`seccomp=unconfined`) |
| Network isolation | 15 | egress is possible (anything but `--network none`) |
| No docker.sock exposure | 15 | the host Docker/OCI socket is bind-mounted |
| Read-only root filesystem | 10 | the root fs is writable |
| No shared host namespaces | 10 | `--pid host` / `--network host` / `--ipc host` |

Grading is **fail-closed**: any dimension the scanner cannot determine is scored
as insecure, never silently passed. Grades map A (>=90) down to F (<50).

## The curated set

The scenarios are captured in a versioned manifest,
[`images.txt`](./images.txt), in three families:

1. **`default-*`** — a popular image (nginx, postgres, redis, mysql, mongo,
   node, python, golang, httpd, rabbitmq, memcached, wordpress) run with a plain
   `docker run` and **zero hardening flags**. This is the baseline the survey is
   about: what you get from a copy-pasted run command.
2. **`naive-*`** — a common but dangerous CI / ops pattern applied to a popular
   base image: a bind-mounted `docker.sock` ("build images in CI"), `--privileged`
   ("docker-in-docker"), and shared host namespaces ("a monitoring sidecar").
3. **`hardened-reference`** — the target every workload should aim for:
   `--user 65532 --cap-drop ALL --security-opt no-new-privileges --read-only
   --tmpfs /tmp --network none`.

Every image is pinned by its **multi-arch manifest-list digest**, so
`docker pull` resolves byte-identical bits on amd64 and arm64. Digests were
captured with:

```bash
docker buildx imagetools inspect <tag> --format '{{.Manifest.Digest}}'
```

## Methodology (so the numbers are defensible)

* **Read-only, config-based.** `ironctl scan` inspects a container's declared
  configuration via `docker inspect`; it never executes the image's real
  workload. To keep each container alive long enough to inspect, the survey
  overrides the entrypoint with `sleep`. This does **not** change any graded
  dimension — user, capabilities, seccomp, network, rootfs, docker.sock and host
  namespaces all come from the image config and the `docker run` flags, not from
  the entrypoint.
* **Declared config, not runtime drops.** The scan sees the *declared* posture.
  An image whose entrypoint drops privileges at runtime (e.g. `gosu`/`su-exec`
  from a root-configured entrypoint, as postgres/mysql do) is still graded on its
  declared root user, because a compromised process reaches the boundary before
  that drop. This is intentional and fail-closed.
* **Runtime-agnostic scores.** The score reflects the container's *config*, not
  the host runtime under it. Running the same config under gVisor (`runsc`) or
  Kata adds real defense-in-depth but does not change these numbers — the survey
  measures the posture the workload declares for itself.
* **Deterministic output.** Rows in `results.md` are sorted by score, so a re-run
  over the same pinned manifest produces a byte-identical table apart from the
  tool-version / timestamp stamp recorded once at the top.

## Reproducing it

Prerequisites: a running Docker daemon. The harness will build `ironctl` from
this repo (needs Go 1.23+ and `CGO_ENABLED=1`) unless you point it at a prebuilt
binary, and uses `python3` (stdlib only) to render the results.

```bash
examples/isolation-survey/survey.sh                 # scan all scenarios
IRONCTL=/path/to/ironctl examples/isolation-survey/survey.sh   # use a prebuilt ironctl
examples/isolation-survey/survey.sh --keep          # leave containers up for poking
```

**Docker Hub rate limits.** Pulling ~12 public images anonymously can hit Docker
Hub's unauthenticated pull-rate limit (HTTP 429). The harness skips the pull for
any digest already cached locally and backs off/retries on a 429, but if you hit
it repeatedly, run `docker login` first (a free account lifts the anonymous
limit). Nothing in the survey needs a paid or private registry.

## Files

| File | What it is |
|------|-----------|
| [`images.txt`](./images.txt) | the pinned, versioned manifest of scenarios |
| [`survey.sh`](./survey.sh) | the harness: pull -> run -> `ironctl scan --json` -> aggregate |
| [`render.py`](./render.py) | stdlib aggregation of scan JSON into `results.{json,md}` |
| [`results.json`](./results.json) | the committed machine-readable dataset |
| [`results.md`](./results.md) | the committed rendered table |

Handing off to Growth for the "State of Container Isolation" writeup that builds
on `results.md`.
