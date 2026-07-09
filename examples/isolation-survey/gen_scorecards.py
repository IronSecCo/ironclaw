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

    print(f"wrote {len(pages)} scorecard pages + index.md to {out_dir}")


if __name__ == "__main__":
    main()
