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

1. **`default-*`** — **50+ of the most-pulled public images** (nginx, postgres,
   redis, mysql, mongo, node, python, golang, mariadb, elasticsearch, grafana,
   prometheus, traefik, vault, alpine, ubuntu, busybox, …) run with a plain
   `docker run` and **zero hardening flags**. This is the baseline the survey is
   about: what you get from a copy-pasted run command, and the set that backs the
   per-image scorecard directory under [`docs/scores/`](../../docs/scores/).
2. **`naive-*`** — a common but dangerous CI / ops pattern applied to a popular
   base image: a bind-mounted `docker.sock` ("build images in CI"), `--privileged`
   ("docker-in-docker"), and shared host namespaces ("a monitoring sidecar").
3. **`hardened-reference`** — the target every workload should aim for:
   `--user 65532 --cap-drop ALL --security-opt no-new-privileges --read-only
   --tmpfs /tmp --network none`.

Most rows are referenced **by tag** and resolve to whatever that tag publishes at
the moment the survey runs; only the original core set (16 of the 295 rows) is
additionally pinned by its **multi-arch manifest-list digest**, so those rows
resolve to the same image index on amd64 and arm64 (digests captured with
`docker buildx imagetools inspect <tag> --format '{{.Manifest.Digest}}'`).
Whichever the row uses, `survey.sh` **records the manifest digest it actually
scanned** into `results.json` (`.scenarios[].resolvedDigest`), so every scorecard
names the exact bits it graded. That is per-run provenance, not a guarantee that
a later re-run reproduces the published scores: re-runs deliberately pick up the
current published tags, because the dataset is about what the ecosystem ships
today.

**Mirror-first pulls.** By default every image is resolved through
`mirror.gcr.io` (Google's pull-through cache for Docker Hub), which is not
anonymously rate-limited — the single biggest cause of a partial survey once the
set grows past a couple dozen images. Set `MIRROR=0` to pull straight from the
original registry. A scenario whose image cannot be pulled or run is **skipped**,
never fatal, so one unavailable image never aborts the survey.

**Coverage is part of the output.** A skip is recorded, not just logged: every
dropped scenario lands in `.skipped[]` of `results.json` as
`{label, image, stage, reason}` — `stage` being `pull`, `run` or `scan` — and is
listed in the "Not scanned" table of [`results.md`](./results.md), alongside
`manifestRowCount` / `scenarioCount` / `skippedCount`. A skipped row is **not
measured**; it is not a low score, and it is not in the table. This exists
because it was previously invisible: the same 39 of 295 rows failed on every
weekly refresh, the only trace was an Actions log that expires, and the run
exited green (IRO-727).

**Losing coverage fails the run.** [`coverage_guard.py`](./coverage_guard.py)
compares the finished run against the previous `results.json` and exits non-zero
if a scenario that scored last time — and is still listed in `images.txt` —
produced nothing now. Deliberately a *regression* check rather than "any skip is
fatal": a transient registry failure is absent from the baseline too, so it
cannot wedge the weekly refresh, while a row that rots for good is caught the
first week it stops scoring. If a row can never be scanned again, the fix is to
delete it from `images.txt` — a label that leaves the manifest is not a
regression. Results are written *before* the guard runs, so a failed run still
leaves the artifact that explains itself.

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
* **Stable output ordering.** Rows in `results.md` are sorted by score, so two
  runs diff as score movement rather than row churn. The rendering step is
  deterministic — re-rendering the same `results.json` is byte-identical apart
  from the tool-version / timestamp stamp recorded once at the top — but the
  *scan* step is not pinned: see "The curated set" above.

## Reproducing it

Prerequisites: a running Docker daemon. The harness will build `ironctl` from
this repo (needs Go 1.23+ and `CGO_ENABLED=1`) unless you point it at a prebuilt
binary, and uses `python3` (stdlib only) to render the results.

```bash
examples/isolation-survey/survey.sh                 # scan all scenarios
IRONCTL=/path/to/ironctl examples/isolation-survey/survey.sh   # use a prebuilt ironctl
examples/isolation-survey/survey.sh --keep          # leave containers up for poking
```

**Docker Hub rate limits.** Pulling 50+ public images anonymously would blow
past Docker Hub's unauthenticated pull-rate limit (HTTP 429), which is exactly
why the harness resolves everything through `mirror.gcr.io` by default (see
"Mirror-first pulls" above). It also skips the pull for any image already cached
locally and backs off/retries on a 429. If you set `MIRROR=0` and hit the limit,
run `docker login` first (a free account lifts the anonymous limit). Nothing in
the survey needs a paid or private registry.

## The per-image scorecard directory

[`gen_scorecards.py`](./gen_scorecards.py) turns `results.json` into an
evergreen SEO directory under [`docs/scores/`](../../docs/scores/): one indexable
page per image with the default-config grade, the full per-dimension breakdown,
the highest-value hardening fixes, and a "scan your own container" CTA. It is
pure stdlib and deterministic — pages are keyed by image slug and regenerating
over the same `results.json` is byte-identical.

```bash
# regenerate the committed scorecard pages from the dataset:
examples/isolation-survey/gen_scorecards.py \
    examples/isolation-survey/results.json docs/scores
```

Adding an image is a one-liner: append it to `images.txt`, rerun `survey.sh`,
then rerun `gen_scorecards.py`. The docs `.nav.yml` `*.md` glob auto-includes the
new page — no manual nav edit.

## Files

| File | What it is |
|------|-----------|
| [`images.txt`](./images.txt) | the versioned manifest of scenarios |
| [`survey.sh`](./survey.sh) | the harness: pull -> run -> `ironctl scan --json` -> aggregate |
| [`render.py`](./render.py) | stdlib aggregation of scan JSON into `results.{json,md}` |
| [`coverage_guard.py`](./coverage_guard.py) | fails a run that lost a scenario the previous run scored |
| [`gen_scorecards.py`](./gen_scorecards.py) | renders `results.json` into `docs/scores/` scorecard pages |
| [`results.json`](./results.json) | the committed machine-readable dataset |
| [`results.md`](./results.md) | the committed rendered table |
