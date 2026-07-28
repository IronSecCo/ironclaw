#!/usr/bin/env python3
"""Fail the docs build when a published page serves the site-wide fallback meta.

MkDocs' ``--strict`` catches broken links and bad nav references. It does not
look at front-matter, so a page that declares no ``title:`` / ``description:``
builds green forever and quietly ships the ``site_name`` / ``site_description``
fallback from ``mkdocs.yml`` as its SERP snippet. That is what IRO-610 found by
hand and IRO-617 found again on two more live pages.

This checks the *built* site rather than globbing ``docs/**/*.md``, on purpose:

* it tests exactly the set of pages that actually ship, so it needs no second
  copy of the ``exclude_docs`` patterns in ``mkdocs.yml`` to drift out of sync;
* it asserts on the served bytes Google reads, not on the presence of a YAML
  key, so it stays correct if the source of a page's meta ever changes.

Usage:

    python3 scripts/check-docs-meta.py _site

Exits 0 when every published page carries its own title and description,
1 when any page falls back, and 2 on a usage/parse error.
"""

from __future__ import annotations

import html
import re
import sys
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent
MKDOCS_YML = REPO_ROOT / "mkdocs.yml"

# Pages that are allowed to serve the fallback meta, relative to the site root.
# 404.html is served with an HTTP 404 status and is never in the sitemap, so it
# has no SERP snippet to get wrong and no page-level front-matter to read.
EXEMPT = frozenset({"404.html"})

TITLE_RE = re.compile(r"<title>(.*?)</title>", re.IGNORECASE | re.DOTALL)
DESCRIPTION_RE = re.compile(
    r"""<meta\s+name=["']description["']\s+content=["'](.*?)["']\s*/?>""",
    re.IGNORECASE | re.DOTALL,
)


class _TolerantLoader(yaml.SafeLoader):
    """SafeLoader that ignores MkDocs' ``!!python/name:`` extension tags.

    ``mkdocs.yml`` wires pymdownx/material extensions with ``!!python/name:``
    tags, which ``yaml.safe_load`` refuses. We only read three scalar keys, so
    resolving those tags to ``None`` is enough and keeps us off ``unsafe_load``.
    """


_TolerantLoader.add_multi_constructor("", lambda loader, suffix, node: None)


def load_site_config() -> tuple[str, str, str]:
    """Return ``(site_name, site_description, site_url)`` from ``mkdocs.yml``."""
    with MKDOCS_YML.open(encoding="utf-8") as handle:
        config = yaml.load(handle, Loader=_TolerantLoader)
    return (
        config.get("site_name", ""),
        config.get("site_description", ""),
        config.get("site_url", ""),
    )


def published_url(site_url: str, relative: Path) -> str:
    """Map a built HTML path to the URL it publishes at."""
    parts = relative.as_posix()
    if parts.endswith("/index.html"):
        parts = parts[: -len("index.html")]
    elif parts == "index.html":
        parts = ""
    return f"{site_url.rstrip('/')}/{parts}"


def source_markdown(relative: Path) -> str:
    """Best-effort map from a built HTML path back to its source Markdown.

    MkDocs flattens ``a/b.md``, ``a/b/index.md`` and ``a/b/README.md`` to the
    same ``a/b/index.html``, so probe all three rather than guessing one.
    """
    stem = relative.as_posix().removesuffix("/index.html").removesuffix(".html")
    if not stem or stem == "index":
        return "docs/index.md"
    for candidate in (f"docs/{stem}.md", f"docs/{stem}/index.md", f"docs/{stem}/README.md"):
        if (REPO_ROOT / candidate).is_file():
            return candidate
    return f"docs/{stem}.md (source not found)"


def extract(pattern: re.Pattern[str], markup: str) -> str | None:
    match = pattern.search(markup)
    return html.unescape(match.group(1)).strip() if match else None


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print(f"usage: {Path(argv[0]).name} <site-dir>", file=sys.stderr)
        return 2

    site_dir = Path(argv[1])
    if not site_dir.is_dir():
        print(f"error: {site_dir} is not a directory", file=sys.stderr)
        return 2

    site_name, site_description, site_url = load_site_config()
    if not site_name or not site_description:
        print("error: mkdocs.yml is missing site_name or site_description", file=sys.stderr)
        return 2

    pages = sorted(site_dir.rglob("*.html"))
    failures: list[str] = []

    for page in pages:
        relative = page.relative_to(site_dir)
        if relative.as_posix() in EXEMPT:
            continue

        markup = page.read_text(encoding="utf-8", errors="replace")
        title = extract(TITLE_RE, markup)
        description = extract(DESCRIPTION_RE, markup)

        reasons = []
        if description is None:
            reasons.append("no <meta name=\"description\">")
        elif description == site_description:
            reasons.append("description is the site_description fallback")
        if title is None:
            reasons.append("no <title>")
        elif title == site_name:
            reasons.append("title is just the site_name")

        if reasons:
            failures.append(
                f"  {relative.as_posix()}\n"
                f"    publishes at: {published_url(site_url, relative)}\n"
                f"    source:       {source_markdown(relative)}\n"
                f"    problem:      {'; '.join(reasons)}"
            )

    if failures:
        print(
            f"Docs front-matter check FAILED: {len(failures)} of {len(pages)} built "
            f"page(s) serve the site-wide fallback meta.\n",
            file=sys.stderr,
        )
        print("\n".join(failures), file=sys.stderr)
        print(
            "\nEvery published page needs its own front-matter, so search engines get a\n"
            "page-specific snippet instead of the site-wide fallback:\n\n"
            "  ---\n"
            '  title: A specific page title\n'
            '  description: One sentence, under 160 characters, describing this page.\n'
            "  ---\n\n"
            "If the page is an internal note rather than documentation, add it to\n"
            "`exclude_docs:` in mkdocs.yml instead so it stops being published at all.",
            file=sys.stderr,
        )
        return 1

    print(f"Docs front-matter check passed: {len(pages)} built page(s), no fallback meta.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
