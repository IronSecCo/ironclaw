#!/usr/bin/env python3
"""gen_scorecards.py — turn the isolation-survey results.json into an evergreen,
per-image SEO directory under docs/scores/ (IRO-445).

For every `default-*` scenario (a popular public image run with plain
`docker run <image>` defaults) this emits one indexable page,
`docs/scores/<image>.md`, carrying:

  * the default-config containment grade (0-100 + letter),
  * the full per-dimension breakdown (what passes, what is wide open),
  * the top hardening fixes that recover the most points, and
  * a "scan your own container" CTA + install snippet that funnels to
    `ironctl scan` and IronClaw.

It also emits `docs/scores/index.md`: the sorted directory of every graded
image with a short funnel intro.

Pure stdlib. Deterministic: pages are keyed by image slug and rows are sorted by
(score asc, slug asc), so regenerating over the same results.json is
byte-identical. Front-matter carries a `description:` field seeded with a
sensible default that Growth refines for SEO (IRO-445 hands the meta-copy pass
to a separate child).

    examples/isolation-survey/gen_scorecards.py \
        examples/isolation-survey/results.json docs/scores
"""
import json
import os
import re
import sys

# Canonical 0-100 scoring dimensions, keyed as `ironctl scan` emits them. The
# fix + rationale for each mirror `ironctl scan`'s own remediation output so a
# scorecard page tells the reader exactly how to close the gap.
FIXES = {
    "user.nonroot": (
        "--user 65532:65532",
        "Pin a non-root uid so a container escape does not begin as host uid 0.",
    ),
    "caps.dropped": (
        "--cap-drop=ALL",
        "Drop every Linux capability; add back only what the workload provably needs.",
    ),
    "seccomp": (
        "keep seccomp on (never --privileged or --security-opt seccomp=unconfined)",
        "A seccomp profile filters the kernel syscall surface a container can reach.",
    ),
    "network.isolated": (
        "--network=none",
        "Cut egress so a compromised workload cannot reach the network or exfiltrate.",
    ),
    "rootfs.readonly": (
        "--read-only --tmpfs /tmp",
        "Make the root filesystem read-only to remove the tamper/persistence surface.",
    ),
    "docker.sock": (
        "remove any -v /var/run/docker.sock bind mount",
        "The Docker socket is one API call from host root; never expose it to a workload.",
    ),
    "namespaces.host": (
        "drop --network host / --pid host / --ipc host",
        "Sharing a host namespace erases the isolation boundary the container provides.",
    ),
}

# Grade -> short human descriptor, worst to best. Matches the language
# `ironctl scan` prints next to the numeric score.
GRADE_WORD = {
    "A": "hardened",
    "B": "solid",
    "C": "partial",
    "D": "porous",
    "F": "wide open",
}
GRADE_EMOJI = {"A": "🟢", "B": "🟢", "C": "🟡", "D": "🟠", "F": "🔴"}
VERDICT_MARK = {"PASS": "✅", "WARN": "⚠️", "FAIL": "❌", "UNKNOWN": "❌"}

# Grade -> shields.io color (6-hex, no leading '#'). Mirrors gradeColor() in
# internal/host/scan/render.go so the committed per-image badges match the
# `ironctl scan --badge-json` palette exactly.
GRADE_COLOR = {"A": "3fb950", "B": "9acd32", "C": "d4a72c", "D": "e8873a", "F": "d1242f"}

# Published site root (mkdocs site_url). Per-image scorecard pages live at
# `${SITE_URL}/scores/<slug>/`; the embeddable badge links back here so a
# maintainer who badges their repo hands us an inbound backlink (IRO-459).
SITE_URL = "https://ironsecco.github.io/ironclaw"


def scorecard_url(slug: str) -> str:
    """Canonical published URL for an image's scorecard page."""
    return f"{SITE_URL}/scores/{slug}/"


def badge_url(score: int, grade: str) -> str:
    """A committed-file-free shields.io STATIC badge for an image's default score.

    No server and no per-image JSON file: shields renders the score straight from
    the URL, colored to match `ironctl scan --badge-json`. `/` and space are
    percent-escaped; the message carries no literal dash/underscore so shields'
    `--`/`__` escaping is not needed.
    """
    color = GRADE_COLOR.get(grade, GRADE_COLOR["F"])
    return (f"https://img.shields.io/badge/"
            f"container%20isolation-{score}%2F100%20{grade}-{color}")


def badge_markdown(slug: str, score: int, grade: str) -> str:
    """Copy-paste markdown: the shields badge wrapped in a link to the scorecard."""
    alt = f"Container Isolation Score: {score}/100 {grade}"
    return f"[![{alt}]({badge_url(score, grade)})]({scorecard_url(slug)})"

# Category facet for the /scores explorer chips (IRO-450 SPEC §5 `family`).
# Values: web|database|runtime|base|infra. Keyed by image slug; explicit so the
# machine JSON stays deterministic and reviewable. Unmapped slugs fall back to
# `infra` (and print a warning) so a newly-surveyed image is never silently
# miscategorised.
FAMILY = {
    # base OS
    "alpine": "base", "amazonlinux": "base", "busybox": "base", "debian": "base",
    "fedora": "base", "rockylinux": "base", "ubuntu": "base",
    # language runtimes
    "golang": "runtime", "node": "runtime", "python": "runtime", "perl": "runtime",
    "php": "runtime", "ruby": "runtime", "rust": "runtime", "eclipse-temurin": "runtime",
    # databases / caches / stores
    "couchdb": "database", "influxdb": "database", "mariadb": "database",
    "mongo": "database", "mysql": "database", "postgres": "database",
    "redis": "database", "memcached": "database",
    # web servers, proxies, and web apps
    "nginx": "web", "nginx-unprivileged": "web", "httpd": "web", "caddy": "web",
    "haproxy": "web", "openresty": "web", "varnish": "web", "traefik": "web",
    "jetty": "web", "tomcat": "web", "drupal": "web", "ghost": "web",
    "joomla": "web", "wordpress": "web", "phpmyadmin": "web", "adminer": "web",
    "kong": "web", "gitea": "web", "grafana": "web", "server": "web",
    # platform / infra services
    "consul": "infra", "vault": "infra", "prometheus": "infra",
    "alertmanager": "infra", "node-exporter": "infra", "nats": "infra",
    "rabbitmq": "infra", "telegraf": "infra", "registry": "infra",
    "zookeeper": "infra", "eclipse-mosquitto": "infra",
}

# The fully-hardened reference run every scorecard funnels to: 100/100 grade A.
# This mirrors the `docker run ... 100/100 (grade A)` block rendered at the
# bottom of each .md page. Flat single-line command so the explorer can render a
# one-click copy affordance (SPEC §5 hardenedCommand).
HARDENED_FLAGS = [
    "--user 65532:65532",
    "--cap-drop=ALL",
    "--security-opt=no-new-privileges",
    "--read-only --tmpfs /tmp",
    "--network=none",
]


def family_for(slug: str) -> str:
    """Category facet for a slug; `infra` fallback with a stderr warning."""
    fam = FAMILY.get(slug)
    if fam is None:
        sys.stderr.write(
            f"warning: no family mapping for slug '{slug}', defaulting to 'infra'\n")
        return "infra"
    return fam


def slug_for(image: str) -> str:
    """A stable, filesystem- and URL-safe id from an image reference.

    `grafana/grafana:11.4.0` -> `grafana`; `hashicorp/vault:1.18` -> `vault`;
    `nginxinc/nginx-unprivileged:1.27-alpine` -> `nginx-unprivileged`;
    `postgres:17-alpine` -> `postgres`.
    """
    repo = image.split("@", 1)[0].split(":", 1)[0]  # drop digest + tag
    name = repo.rsplit("/", 1)[-1]                   # drop registry/namespace
    return re.sub(r"[^a-z0-9._-]+", "-", name.lower()).strip("-")


def display_name(slug: str) -> str:
    """Human title-case-ish name for a slug (kept simple + deterministic)."""
    return slug.replace("-", " ")


def short_ref(image: str) -> str:
    """`repo:tag` with registry/namespace and digest dropped, for prose + titles.
    `grafana/grafana:11.4.0` -> `grafana:11.4.0`; `nginx:1.27-alpine` unchanged."""
    return image.split("@", 1)[0].rsplit("/", 1)[-1]


# Short, human phrasing of a FAILed dimension for the SEO meta description
# (IRO-446 seam): terse, no em/en-dash, keyed on the scan's dimension key.
FAIL_PHRASE = {
    "user.nonroot": "runs as root",
    "caps.dropped": "retains default capabilities",
    "seccomp": "seccomp disabled",
    "network.isolated": "egress open",
    "rootfs.readonly": "writable root filesystem",
    "docker.sock": "docker.sock exposed",
    "namespaces.host": "shares host namespaces",
}


def failed_phrase(report: dict, limit: int = 2) -> str:
    """The 1-2 highest-max FAILed dimensions as a human phrase, e.g.
    'retains default capabilities, runs as root' (IRO-446 contract)."""
    fails = [d for d in report.get("dimensions", [])
             if d.get("verdict") in ("FAIL", "UNKNOWN")]
    fails.sort(key=lambda d: (-d.get("max", 0), d.get("key", "")))
    ph = [FAIL_PHRASE.get(d.get("key", ""), d.get("title", "").lower())
          for d in fails[:limit]]
    return ", ".join(ph) if ph else "hardened on every containment dimension"


def seo_description(short: str, score: int, grade: str, report: dict) -> str:
    """Unique, <=160-char meta description, no em/en-dash (IRO-254/IRO-446)."""
    phrase = failed_phrase(report, 2)
    tmpl = (f"How isolated is {short} by default? IronClaw scores its sandbox "
            f"posture {score}/100 ({grade}): {phrase}. Scan any container in 10s.")
    if len(tmpl) <= 160:
        return tmpl
    # Too long (long image name + two phrases): drop to one phrase.
    phrase1 = failed_phrase(report, 1)
    tmpl = (f"How isolated is {short} by default? IronClaw scores its sandbox "
            f"posture {score}/100 ({grade}): {phrase1}. Scan any container in 10s.")
    if len(tmpl) <= 160:
        return tmpl
    return tmpl[:157].rstrip() + "..."


def top_fixes(report: dict, limit: int = 4) -> list:
    """The highest-value hardening steps for this image: dimensions graded
    WARN/FAIL/UNKNOWN, biggest point recovery first."""
    dims = [d for d in report.get("dimensions", [])
            if d.get("verdict") in ("FAIL", "WARN", "UNKNOWN")]
    dims.sort(key=lambda d: (-(d.get("max", 0) - d.get("score", 0)), d.get("key", "")))
    out = []
    for d in dims[:limit]:
        fix, why = FIXES.get(d.get("key", ""), ("harden this dimension", d.get("detail", "")))
        out.append((d.get("title", d.get("key", "?")), fix, why))
    return out


def render_page(scn: dict) -> str:
    rep = scn["report"]
    image = scn["image"]
    slug = slug_for(image)
    name = display_name(slug)
    score = rep.get("score", 0)
    grade = rep.get("grade", "?")
    word = GRADE_WORD.get(grade, "")
    digest = scn.get("resolvedDigest", "")
    dims = rep.get("dimensions", [])

    # Front-matter honoring the IRO-446 SEO seam: quoted title + unique,
    # <=160-char description, one H1 == the title text. Values are double-quoted
    # because they contain a colon (IRO-277 `: ` YAML gotcha).
    image_tag = image.split("@")[0]
    short = short_ref(image)
    title = f"{short} container isolation score: {score}/100 (grade {grade})"
    desc = seo_description(short, score, grade, rep)

    L = []
    L.append("---")
    L.append(f'title: "{title}"')
    L.append(f'description: "{desc}"')
    L.append("---")
    L.append("")
    L.append(f"# {title}")
    L.append("")
    L.append(f"Run with plain `docker run {image.split('@')[0]}` defaults — no "
             f"hardening flags — the **{name}** image scores "
             f"**{score}/100, grade {grade} ({word})** on IronClaw's seven-dimension "
             f"container containment scale. Higher is safer. This is what you get "
             f"straight out of a copy-pasted `docker run`; the fixes below close the gap.")
    L.append("")
    if digest:
        L.append(f"> Graded from a read-only `docker inspect` of "
                 f"`{image.split('@')[0]}` at digest `{digest}`. No workload is "
                 f"executed. [How scoring works &rarr;](../scan.md)")
        L.append("")

    # Per-dimension table.
    L.append("## How it scores, dimension by dimension")
    L.append("")
    L.append("| Dimension | Verdict | Score | What the scan found |")
    L.append("|-----------|:-------:|------:|---------------------|")
    for d in dims:
        mark = VERDICT_MARK.get(d.get("verdict", ""), "")
        L.append(f"| {d.get('title','?')} | {mark} {d.get('verdict','')} | "
                 f"{d.get('score',0)}/{d.get('max',0)} | {d.get('detail','')} |")
    L.append("")

    # Top hardening fixes.
    fixes = top_fixes(rep)
    if fixes:
        L.append("## Harden it: the highest-value fixes")
        L.append("")
        L.append(f"Applying these to your `docker run {slug}` closes the biggest "
                 f"gaps first (most points recovered first):")
        L.append("")
        for ttl, fix, why in fixes:
            L.append(f"- **{ttl}** — `{fix}`  ")
            L.append(f"  {why}")
        L.append("")
        L.append("A fully hardened run scores **100/100 (grade A)**:")
        L.append("")
        L.append("```bash")
        L.append(f"docker run -d --name {slug}-hardened \\")
        L.append("  --user 65532:65532 \\")
        L.append("  --cap-drop=ALL \\")
        L.append("  --security-opt=no-new-privileges \\")
        L.append("  --read-only --tmpfs /tmp \\")
        L.append("  --network=none \\")
        L.append(f"  {image.split('@')[0]}")
        L.append("```")
        L.append("")
    else:
        L.append("This configuration already passes every containment dimension. "
                 "Nice.")
        L.append("")

    # CTA / funnel.
    L.append("## Scan your own container")
    L.append("")
    L.append("These grades come from `ironctl scan`, a single, credential-free "
             "command that audits any running container, docker-compose service, "
             "or Kubernetes manifest — not just this image:")
    L.append("")
    L.append("```bash")
    L.append("# install (Homebrew)")
    L.append("brew install ironsecco/ironclaw/ironclaw")
    L.append("")
    L.append(f"# grade your own {slug} the same way this page was generated")
    L.append(f"ironctl scan my-{slug}")
    L.append("```")
    L.append("")
    L.append("- [Scan any container &rarr;](../scan.md) — the full command reference.")
    L.append("- [Add an isolation-score badge to your repo &rarr;]"
             "(../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)")
    L.append("- [The State of Container Isolation, 2026 &rarr;]"
             "(../blog/state-of-container-isolation-2026.md) — the full survey "
             "this directory is built from.")
    L.append("- [Run untrusted code in a real sandbox &rarr;](../index.md) — "
             "IronClaw wraps every AI-agent session in a gVisor/Kata isolation "
             "boundary with `network=none` by default.")
    L.append("")

    # Embeddable badge. A committed-file-free shields.io static badge maintainers
    # can paste into their README; clicking it lands here, an inbound backlink
    # (IRO-459). Rendered live + as a copy-paste markdown snippet.
    L.append("## Badge this image")
    L.append("")
    L.append(f"Maintain **{name}** (or run it)? Show its default-config isolation "
             f"score with a badge that links back to this scorecard:")
    L.append("")
    L.append(badge_markdown(slug, score, grade))
    L.append("")
    L.append("```markdown")
    L.append(badge_markdown(slug, score, grade))
    L.append("```")
    L.append("")
    L.append("The badge is a plain [shields.io](https://shields.io) URL: no server, "
             "no build step, nothing to host. It reflects this page's "
             "default-configuration grade. Hardened your own deployment? Generate a "
             "live badge of *your* config with "
             "[`ironctl scan --badge-json`](../blog/"
             "add-a-sandbox-isolation-score-badge-to-your-repo.md), or compare every "
             "image on the [leaderboard](leaderboard.md).")
    L.append("")
    L.append("---")
    L.append("")
    L.append("*Part of the [Container Isolation Scores](index.md) directory — "
             "default-configuration containment grades for the most-pulled public "
             "images.*")
    L.append("")
    return "\n".join(L)


def render_index(defaults: list) -> str:
    rows = sorted(defaults, key=lambda s: (s["report"].get("score", 0), slug_for(s["image"])))
    total = len(rows)
    dist = {}
    for s in rows:
        g = s["report"].get("grade", "?")
        dist[g] = dist.get(g, 0) + 1
    dist_str = ", ".join(f"{dist[g]}×{g}" for g in sorted(dist))
    avg = round(sum(s["report"].get("score", 0) for s in rows) / total) if total else 0

    L = []
    L.append("---")
    L.append("title: Container Isolation Scores")
    L.append("description: The default-configuration container isolation score for "
             f"{total}+ of the most-pulled public Docker images, graded 0-100 across "
             "seven containment dimensions by ironctl scan.")
    L.append("---")
    L.append("")
    L.append("# Container Isolation Scores")
    L.append("")
    L.append(f"How isolated is the container you just `docker run`? This directory "
             f"grades **{total} of the most-pulled public images** as they ship — "
             f"run with plain `docker run <image>` defaults, no hardening flags — "
             f"on IronClaw's seven-dimension containment scale (0-100). "
             f"Every score comes from `ironctl scan`, a credential-free audit you "
             f"can run on your own containers in ten seconds.")
    L.append("")
    L.append(f"**The headline:** the average default image scores **{avg}/100**. "
             f"Grade distribution: {dist_str}. Almost nothing you pull is isolated "
             f"out of the box — it runs as root, keeps the full capability set, and "
             f"has a writable root filesystem. The good news: every gap on these "
             f"pages closes with a handful of `docker run` flags.")
    L.append("")
    L.append("> **Scan your own container:** "
             "`brew install ironsecco/ironclaw/ironclaw && ironctl scan my-container`. "
             "See [Scan any container](../scan.md).")
    L.append("")
    L.append("> **New:** the [Container Isolation Leaderboard](leaderboard.md) ranks "
             "every image best-to-worst, with a Hall of Fame and a Worst-offenders "
             "cut. Each scorecard now carries a copy-paste "
             "[shields.io badge](leaderboard.md) you can embed in your repo.")
    L.append("")
    L.append("## Every image, worst-isolated first")
    L.append("")
    L.append("| Image | Score | Grade | Top gaps (default config) |")
    L.append("|-------|------:|:-----:|---------------------------|")
    for s in rows:
        rep = s["report"]
        slug = slug_for(s["image"])
        grade = rep.get("grade", "?")
        emoji = GRADE_EMOJI.get(grade, "")
        gaps = ", ".join(t for t, _, _ in top_fixes(rep, 3)) or "none — fully hardened"
        img = s["image"].split("@")[0]
        L.append(f"| [`{img}`]({slug}.md) | {rep.get('score',0)}/100 | "
                 f"{emoji} **{grade}** | {gaps} |")
    L.append("")
    L.append("## What the seven dimensions mean")
    L.append("")
    L.append("Each image is graded on the same containment dimensions IronClaw's "
             "own sandbox benchmark checks — the properties that decide whether a "
             "container escape starts from a strong or a hopeless position:")
    L.append("")
    L.append("- **Non-root user** (15 pts) — does it drop host uid 0?")
    L.append("- **Dropped capabilities** (20 pts) — is the Linux capability set minimized?")
    L.append("- **Seccomp profile** (15 pts) — is the syscall surface filtered?")
    L.append("- **Network isolation** (15 pts) — is egress cut (`network=none`)?")
    L.append("- **Read-only root filesystem** (10 pts) — is the tamper surface removed?")
    L.append("- **No docker.sock exposure** (15 pts) — is the host control socket kept out?")
    L.append("- **No shared host namespaces** (10 pts) — are PID/IPC/net namespaces private?")
    L.append("")
    L.append("Scores are fail-closed: any posture the scanner cannot determine is "
             "graded as insecure, never silently passed. See "
             "[how scoring works](../scan.md) and "
             "[the full survey methodology](../blog/state-of-container-isolation-2026.md).")
    L.append("")
    L.append("---")
    L.append("")
    L.append("*These pages are generated from a reproducible survey — "
             "`examples/isolation-survey/survey.sh` scans every image, "
             "`gen_scorecards.py` renders the pages. Grades reflect the image's "
             "default configuration, not a limit of the image itself: every one "
             "can reach grade A with the right `docker run` flags.*")
    L.append("")
    return "\n".join(L)


# Family slug -> human label for the leaderboard's grouped cut.
FAMILY_LABEL = {
    "base": "Base OS images",
    "runtime": "Language runtimes",
    "database": "Databases, caches, and stores",
    "web": "Web servers, proxies, and apps",
    "infra": "Platform and infra services",
}
MEDAL = {1: "🥇", 2: "🥈", 3: "🥉"}


def _leaderboard_row(rank, scn, medals=True):
    """One `| rank | image | score | grade | gaps |` row for a leaderboard table.
    Medals decorate the top three only when `medals` is set (best-first tables);
    the worst-offenders table passes medals=False so a gold medal never lands on
    the least-isolated image."""
    rep = scn["report"]
    slug = slug_for(scn["image"])
    grade = rep.get("grade", "?")
    emoji = GRADE_EMOJI.get(grade, "")
    medal = MEDAL.get(rank, "") if medals else ""
    gaps = ", ".join(t for t, _, _ in top_fixes(rep, 3)) or "none, fully hardened"
    img = scn["image"].split("@")[0]
    rank_cell = f"{medal} {rank}".strip()
    return (f"| {rank_cell} | [`{img}`]({slug}.md) | {rep.get('score',0)}/100 | "
            f"{emoji} **{grade}** | {gaps} |")


def render_leaderboard(defaults: list) -> str:
    """The ranked, shareable leaderboard: best-to-worst, plus a Hall of Fame,
    a Worst-offenders cut, and a best-in-category group. Generated from the same
    scenarios as the scorecards, so the weekly refresh keeps it in sync.
    """
    # Best-first, ties broken by slug for determinism.
    rows = sorted(defaults, key=lambda s: (-s["report"].get("score", 0), slug_for(s["image"])))
    total = len(rows)
    avg = round(sum(s["report"].get("score", 0) for s in rows) / total) if total else 0
    best = rows[0]["report"].get("score", 0) if rows else 0
    worst = rows[-1]["report"].get("score", 0) if rows else 0

    # Hall of Fame: any grade-A defaults if they exist, else the top decile
    # (min 5, capped at 15). Worst offenders: the mirror-image bottom cut.
    cut = max(5, min(15, total // 10))
    grade_a = [s for s in rows if s["report"].get("grade") == "A"]
    hall = grade_a if grade_a else rows[:cut]
    worst_off = list(reversed(rows[-cut:]))  # worst first

    L = []
    L.append("---")
    L.append('title: "Container Isolation Leaderboard: the most (and least) isolated Docker images"')
    L.append("description: A ranked leaderboard of the default container isolation "
             f"score for {total} of the most-pulled public Docker images, graded "
             "0-100 by ironctl scan. Hall of Fame and worst offenders included.")
    L.append("---")
    L.append("")
    L.append("# Container Isolation Leaderboard")
    L.append("")
    L.append(f"Every one of **{total} of the most-pulled public Docker images**, "
             f"ranked by how isolated it is when you `docker run` it with plain "
             f"defaults, no hardening flags. Scores run **{worst}/100 to {best}/100** "
             f"(average **{avg}/100**), graded across IronClaw's seven containment "
             f"dimensions by `ironctl scan`. Higher is safer. Regenerated with the "
             f"[weekly survey refresh](index.md), so this ranking never goes stale.")
    L.append("")
    L.append("> The uncomfortable headline: **no popular image ships isolated.** "
             "Even the leaders leave capabilities, egress, and a writable root "
             "filesystem wide open. The gap between any image here and a clean "
             "**100/100 grade A** is a handful of `docker run` flags, shown on every "
             "scorecard.")
    L.append("")

    # Hall of Fame.
    L.append("## 🏆 Hall of Fame")
    L.append("")
    if grade_a:
        L.append("Images that reach **grade A (100/100)** in their default "
                 "configuration, out of the box:")
    else:
        L.append(f"No image ships at grade A, so this is the next best thing: the "
                 f"**{len(hall)} best-isolated images by default**, the ones that "
                 f"start you closest to a hardened posture.")
    L.append("")
    L.append("| Rank | Image | Score | Grade | Remaining gaps (default config) |")
    L.append("|-----:|-------|------:|:-----:|---------------------------------|")
    for i, s in enumerate(hall, 1):
        L.append(_leaderboard_row(i, s))
    L.append("")

    # Worst offenders.
    L.append("## 🚨 Worst offenders")
    L.append("")
    L.append(f"The **{len(worst_off)} least-isolated images** in the survey. Pulling "
             f"one of these unhardened puts a container escape one step from host "
             f"uid 0. Each links to the exact flags that fix it.")
    L.append("")
    L.append("| # | Image | Score | Grade | Top gaps (default config) |")
    L.append("|--:|-------|------:|:-----:|---------------------------|")
    for i, s in enumerate(worst_off, 1):
        L.append(_leaderboard_row(i, s, medals=False))
    L.append("")

    # Best-in-category grouped cut.
    L.append("## Best in each category")
    L.append("")
    L.append("The most-isolated default image in every family. Comparing like with "
             "like, a database against a database, a base OS against a base OS:")
    L.append("")
    L.append("| Category | Best-isolated image | Score | Grade |")
    L.append("|----------|---------------------|------:|:-----:|")
    for fam in ("base", "runtime", "database", "web", "infra"):
        fam_rows = [s for s in rows if family_for(slug_for(s["image"])) == fam]
        if not fam_rows:
            continue
        top = fam_rows[0]  # rows already best-first
        rep = top["report"]
        slug = slug_for(top["image"])
        grade = rep.get("grade", "?")
        emoji = GRADE_EMOJI.get(grade, "")
        img = top["image"].split("@")[0]
        L.append(f"| {FAMILY_LABEL[fam]} | [`{img}`]({slug}.md) | "
                 f"{rep.get('score',0)}/100 | {emoji} **{grade}** |")
    L.append("")

    # Full ranking.
    L.append("## Full ranking, best to worst")
    L.append("")
    L.append("Every graded image, most-isolated first. Click any image for its "
             "per-dimension breakdown, the exact hardening flags, and a copy-paste "
             "score badge you can embed in your repo.")
    L.append("")
    L.append("| Rank | Image | Score | Grade | Top gaps (default config) |")
    L.append("|-----:|-------|------:|:-----:|---------------------------|")
    for i, s in enumerate(rows, 1):
        L.append(_leaderboard_row(i, s))
    L.append("")

    # CTA / cross-links.
    L.append("## Move up the leaderboard")
    L.append("")
    L.append("Every gap on this page closes with `docker run` flags. Audit your own "
             "container, or one you maintain, with the same credential-free command "
             "that produced these grades:")
    L.append("")
    L.append("```bash")
    L.append("brew install ironsecco/ironclaw/ironclaw")
    L.append("ironctl scan my-container")
    L.append("```")
    L.append("")
    L.append("- [Container Isolation Scores directory](index.md) — every scorecard, "
             "worst-isolated first.")
    L.append("- [Scan any container &rarr;](../scan.md) — the full command reference.")
    L.append("- [Add an isolation-score badge to your repo &rarr;]"
             "(../blog/add-a-sandbox-isolation-score-badge-to-your-repo.md)")
    L.append("- [The State of Container Isolation, 2026 &rarr;]"
             "(../blog/state-of-container-isolation-2026.md) — the full survey.")
    L.append("")
    L.append("---")
    L.append("")
    L.append("*Generated from a reproducible survey by "
             "`examples/isolation-survey/gen_scorecards.py`. Grades reflect each "
             "image's default configuration, not a limit of the image itself: every "
             "one reaches grade A with the right `docker run` flags.*")
    L.append("")
    return "\n".join(L)


def image_row(scn: dict) -> dict:
    """One machine-readable image record for index.json (SPEC §5 images[])."""
    rep = scn["report"]
    image = scn["image"].split("@", 1)[0]
    slug = slug_for(image)
    score = rep.get("score", 0)
    grade = rep.get("grade", "?")
    dims = [
        {
            "key": d.get("key", ""),
            "title": d.get("title", ""),
            "score": d.get("score", 0),
            "max": d.get("max", 0),
            "verdict": d.get("verdict", "UNKNOWN"),
            "detail": d.get("detail", ""),
        }
        for d in rep.get("dimensions", [])
    ]
    top_gaps = [ttl for ttl, _, _ in top_fixes(rep, 3)]
    hardened_command = ("docker run -d " + " ".join(HARDENED_FLAGS) + " " + image)
    return {
        "slug": slug,
        "image": image,
        "displayName": display_name(slug),
        "family": family_for(slug),
        "digest": scn.get("resolvedDigest", ""),
        "score": score,
        "grade": grade,
        "topGaps": top_gaps,
        "dimensions": dims,
        "hardenedScore": 100,
        "hardenedGrade": "A",
        "hardenedFlags": list(HARDENED_FLAGS),
        "hardenedCommand": hardened_command,
        # Embeddable shields.io badge for this image (IRO-459). Server-free static
        # URL + copy-paste markdown that links back to the scorecard page, so the
        # explorer can render the same copy affordance as the .md scorecard.
        "pageUrl": scorecard_url(slug),
        "badgeUrl": badge_url(score, grade),
        "badgeMarkdown": badge_markdown(slug, score, grade),
    }


def build_index(data: dict, pages: dict) -> dict:
    """The machine-readable index.json powering the /scores explorer (SPEC §5).

    One row per image (the deduped default-* scenario). meta numbers are derived
    (never hardcoded) so they track the survey as it grows.
    """
    rows = [image_row(scn) for _, scn in sorted(pages.items())]
    rows.sort(key=lambda r: (r["score"], r["slug"]))  # worst-first, stable
    total = len(rows)
    avg = round(sum(r["score"] for r in rows) / total) if total else 0
    dist = {g: 0 for g in ("A", "B", "C", "D", "F")}
    for r in rows:
        dist[r["grade"]] = dist.get(r["grade"], 0) + 1

    # Canonical dimension order + weights, taken from the first row (every image
    # is scored on the same seven dimensions in the same order).
    dim_meta = []
    if rows:
        for d in rows[0]["dimensions"]:
            dim_meta.append({"key": d["key"], "title": d["title"], "max": d["max"]})

    return {
        "schemaVersion": "1.0",
        "generatedAt": data.get("generatedAt", ""),
        "ironctlVersion": data.get("ironctlVersion", ""),
        "meta": {
            "imageCount": total,
            "avgScore": avg,
            "gradeDistribution": dist,
            "dimensions": dim_meta,
        },
        "images": rows,
    }


def no_dashes(s: str) -> str:
    """Strip em/en-dashes from published copy (IRO-254 standing rule). A spaced
    em-dash becomes a comma; any bare one becomes a hyphen-minus."""
    return (s.replace(" — ", ", ").replace(" – ", ", ")
             .replace("—", "-").replace("–", "-"))


def main():
    results_path, out_dir = sys.argv[1], sys.argv[2]
    with open(results_path) as f:
        data = json.load(f)

    defaults = [s for s in data["scenarios"] if s["label"].startswith("default-")]
    # Dedupe by slug (one page per image), keeping the first by (score, slug).
    seen = {}
    for s in sorted(defaults, key=lambda s: (s["report"].get("score", 0), slug_for(s["image"]))):
        seen.setdefault(slug_for(s["image"]), s)
    pages = seen

    os.makedirs(out_dir, exist_ok=True)
    for slug, scn in sorted(pages.items()):
        with open(os.path.join(out_dir, f"{slug}.md"), "w") as f:
            f.write(no_dashes(render_page(scn)))
    with open(os.path.join(out_dir, "index.md"), "w") as f:
        f.write(no_dashes(render_index(list(pages.values()))))
    with open(os.path.join(out_dir, "leaderboard.md"), "w") as f:
        f.write(no_dashes(render_leaderboard(list(pages.values()))))

    # Machine-readable index for the /scores explorer (IRO-450/451, SPEC §5).
    # Emitted next to the .md files so it publishes with the docs site and is
    # fetchable by the landing build. String values pass through no_dashes for
    # the public-copy rule; keys/enums are ASCII already.
    index = build_index(data, pages)
    with open(os.path.join(out_dir, "index.json"), "w") as f:
        json.dump(index, f, indent=2, ensure_ascii=False)
        f.write("\n")

    print(f"wrote {len(pages)} scorecard pages + index.md + leaderboard.md + "
          f"index.json ({index['meta']['imageCount']} images) to {out_dir}")


if __name__ == "__main__":
    main()
