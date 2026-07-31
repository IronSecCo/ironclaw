#!/usr/bin/env python3
"""Fail the docs build when a hardening guide is not wired into its indexes.

A guide that exists on disk but is listed nowhere is invisible: it never appears
in the site nav, nothing links to it, and no check notices. ``mkdocs build
--strict`` and ``scripts/check_links.py`` both only look at links that *exist* --
a *missing* entry is not a broken link, so neither one can see this. That is
exactly how IRO-696 was found: by an outside contributor, by hand, not by CI.

Two surfaces are required to be exhaustive, and this asserts both:

* ``docs/blog/.nav.yml`` -- the nav is code (``docs/hooks.py::_assemble_nav``),
  and a guide absent from it is unreachable by navigation.
* ``docs/blog/hardening-guides.md`` -- the hub page, which opens with the words
  "Every guide below", so an omission there makes the page's own claim false.

``docs/blog/index.md`` and ``README.md`` are deliberately *not* checked. Neither
claims, or should claim, that every guide appears; both instead link the hub,
which is exhaustive. ``index.md`` gives each entry a hand-written summary quoting
real before/after scores, so enforcing presence there would demand a fabricated
summary per guide, and a check that pressures an author into inventing scores is
worse than no check. ``README.md`` carries a short, representative link list: it
is the front door, not a catalog, and it stays short on purpose.

The contributor-facing statement of this rule is in CONTRIBUTING.md, under
"Adding a hardening guide: which indexes are exhaustive" (IRO-711). Keep the two
in sync.

Usage:

    python3 scripts/check-guide-index.py [repo-root]

Exits 0 when every guide is wired into both surfaces, 1 when any is missing, and
2 on a usage/IO error. Negative controls: scripts/tests/test_check_guide_index.py.
"""

from __future__ import annotations

import sys
from pathlib import Path

# Every doc matching this glob is a published hardening guide and must be wired
# into each SURFACES entry below.
GUIDE_GLOB = "harden-*.md"

# (path relative to repo root, human name used in the failure message).
SURFACES = [
    (Path("docs/blog/.nav.yml"), "the blog nav"),
    (Path("docs/blog/hardening-guides.md"), "the hardening-guides hub"),
]

BLOG_DIR = Path("docs/blog")


def main(argv: list[str]) -> int:
    if len(argv) > 2:
        print(f"usage: {argv[0]} [repo-root]", file=sys.stderr)
        return 2
    root = Path(argv[1]) if len(argv) == 2 else Path.cwd()

    blog = root / BLOG_DIR
    if not blog.is_dir():
        print(f"::error::{blog} is not a directory", file=sys.stderr)
        return 2

    guides = sorted(p.name for p in blog.glob(GUIDE_GLOB))
    # A glob that matches nothing would make every assertion below pass over an
    # empty set and report a green "all guides wired". Refuse to be that check.
    if not guides:
        print(
            f"::error::no {GUIDE_GLOB} guides found under {blog}; "
            "this check would pass vacuously",
            file=sys.stderr,
        )
        return 2

    texts = {}
    for rel, _name in SURFACES:
        path = root / rel
        try:
            texts[rel] = path.read_text(encoding="utf-8")
        except OSError as exc:
            print(f"::error::cannot read {rel}: {exc}", file=sys.stderr)
            return 2

    missing = []
    for guide in guides:
        for rel, name in SURFACES:
            if guide not in texts[rel]:
                missing.append((guide, rel, name))

    if missing:
        for guide, rel, name in missing:
            print(
                f"::error file={BLOG_DIR / guide}::{guide} is not referenced from "
                f"{rel} ({name}); add it there or the guide ships invisible",
                file=sys.stderr,
            )
        print(
            f"\n{len(missing)} missing reference(s) across {len(guides)} guide(s).",
            file=sys.stderr,
        )
        return 1

    # Name the count that was actually enumerated: "no failures" and "nothing was
    # checked" have to be distinguishable in the log.
    print(
        f"ok: {len(guides)} hardening guides, each referenced from "
        f"{' and '.join(rel.name for rel, _ in SURFACES)}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
