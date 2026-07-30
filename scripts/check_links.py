#!/usr/bin/env python3
"""Validate relative links and anchor fragments in the repository's root Markdown.

Scope: the ``*.md`` files at the repository root (README.md, CONTRIBUTING.md,
SECURITY.md, ...). These are the files MkDocs never sees, so nothing else in CI
checks them.

``docs/**`` is deliberately NOT scanned here. MkDocs validates it natively via
``validation.links.anchors: warn`` in mkdocs.yml, which resolves anchors against
the real rendered heading tree instead of a reimplementation of the slug rules.
Two checkers over the same files with two different slug algorithms is a
false-positive generator, and a docs gate that cries wolf gets switched off.

Anchor slugs here follow **github-slugger**, not python-markdown, because root
Markdown is rendered by GitHub. The two disagree: GitHub keeps runs of hyphens
(``the exact `--fix` `` -> ``the-exact---fix``) where python-markdown collapses
them (``the-exact-fix``). Using the wrong one is how a link checker reports a
working link as broken.

Exit status is 0 when every relative link resolves, 1 otherwise.

Originally contributed by @Phoenix1504e in #635; rescoped and corrected in
IRO-688.
"""

from __future__ import annotations

import re
import sys
import unicodedata
from pathlib import Path

# Fenced code blocks and HTML comments are stripped before anything else. A
# shell comment inside a fence (`# --addr defaults to ...`) is not a heading,
# and a commented-out template (docs/scan-coverage.md's card contract) is not a
# link. Parsing them as such is what forced the hardcoded `<deep doc>` skip this
# script used to carry.
FENCED_BLOCK_RE = re.compile(r'^([ \t]*)(```+|~~~+).*?^\1\2[^\n]*$', re.MULTILINE | re.DOTALL)
HTML_COMMENT_RE = re.compile(r'<!--.*?-->', re.DOTALL)

# Inline links: [text](url). Skips absolute URLs and mailto:.
INLINE_LINK_RE = re.compile(r'\[([^\]]+)\]\((?!https?:|mailto:|#?\))([^)\s]*)')
# Reference definitions: [ref]: url
REF_DEF_RE = re.compile(
    r'^[ \t]*\[([^\]]+)\]:[ \t]*(?!https?:|mailto:|<?https?:)'
    r'<?([\w\-./]+(?:#[\w\-.]*)?|#[\w\-.]+)>?'
    r'(?:[ \t]+["\'(].*?["\')])?[ \t]*$',
    re.MULTILINE,
)
# HTML hrefs: href="url"
HTML_HREF_RE = re.compile(r'href=["\'](?!https?:|mailto:|#?["\'])([^"\']+)["\']', re.IGNORECASE)

HEADING_RE = re.compile(r'^[ \t]*(#{1,6})[ \t]+(.*?)[ \t]*#*[ \t]*$', re.MULTILINE)
HTML_TAG_RE = re.compile(r'<[!/a-z][^>]*>', re.IGNORECASE)
ATTR_LIST_ID_RE = re.compile(r'\{[^}]*#([a-zA-Z0-9\-_]+)[^}]*\}')
HTML_ANCHOR_RE = re.compile(r'<[a-zA-Z0-9\-]+[^>]+?(?:id|name)=["\']([^"\']+)["\']', re.IGNORECASE)

# github-slugger drops punctuation, symbols and control characters -- including
# non-ASCII ones, so an em dash goes too -- but keeps `-` and `_`, then turns
# each space into a hyphen. It does not collapse hyphen runs and does not trim
# them, which is the whole difference from python-markdown's slugify.
SLUG_KEEP = frozenset('-_')


def github_slug(heading: str) -> str:
    """Approximate github-slugger over a heading's rendered text."""
    text = HTML_TAG_RE.sub('', heading).lower()
    kept = [
        ch for ch in text
        if ch in SLUG_KEEP or unicodedata.category(ch)[0] not in ('C', 'P', 'S')
    ]
    return ''.join(kept).replace(' ', '-')


def anchors_for(path: Path) -> set[str]:
    """Every fragment that resolves inside ``path``."""
    try:
        content = path.read_text(encoding='utf-8')
    except (OSError, UnicodeDecodeError):
        return set()

    body = HTML_COMMENT_RE.sub('', FENCED_BLOCK_RE.sub('', content))
    anchors: set[str] = set()

    for match in HEADING_RE.finditer(body):
        raw = match.group(2)
        anchors.add(github_slug(raw))
        # A `{ #explicit-id }` is literal text to GitHub but a real anchor to
        # MkDocs. docs/ pages linked from root files are rendered both ways, so
        # accept it rather than emit a false positive.
        attr = ATTR_LIST_ID_RE.search(raw)
        if attr:
            anchors.add(attr.group(1))

    for match in HTML_ANCHOR_RE.finditer(body):
        anchors.add(match.group(1))

    return anchors


def line_of(content: str, offset: int) -> int:
    return content.count('\n', 0, offset) + 1


def collect_links(content: str) -> list[tuple[str, str, str, int]]:
    """(link_text, url, kind, line) for every relative link in ``content``."""
    found = []
    for match in INLINE_LINK_RE.finditer(content):
        found.append((match.group(1), match.group(2), 'inline link', line_of(content, match.start())))
    for match in REF_DEF_RE.finditer(content):
        found.append((match.group(1), match.group(2), 'reference link', line_of(content, match.start())))
    for match in HTML_HREF_RE.finditer(content):
        found.append(('href', match.group(1), 'HTML href', line_of(content, match.start())))
    return found


def check(repo_root: Path) -> int:
    targets = sorted(repo_root.glob('*.md'))
    if not targets:
        print(f'error: no root *.md files found under {repo_root}', file=sys.stderr)
        return 1

    anchor_cache: dict[Path, set[str]] = {}
    failures = 0
    checked = 0

    for source in targets:
        raw = source.read_text(encoding='utf-8')
        body = HTML_COMMENT_RE.sub('', FENCED_BLOCK_RE.sub('', raw))

        for link_text, url, kind, line in collect_links(body):
            url = url.strip()
            if not url or url.startswith('<'):
                continue

            path_part, _, fragment = url.partition('#')
            checked += 1

            if path_part:
                target = (source.parent / path_part).resolve()
            else:
                target = source

            if not target.exists():
                print(f'{source.name}:{line}: [file missing] ({kind}) '
                      f'"{link_text}" -> {path_part!r} does not exist')
                failures += 1
                continue

            if not fragment:
                continue

            if target.suffix.lower() != '.md':
                # Anchors inside non-Markdown targets are not ours to resolve.
                continue

            if target not in anchor_cache:
                anchor_cache[target] = anchors_for(target)

            if fragment not in anchor_cache[target]:
                rel = target.relative_to(repo_root) if repo_root in target.parents else target.name
                print(f'{source.name}:{line}: [anchor broken] ({kind}) '
                      f'"{link_text}" -> #{fragment} not found in {rel}')
                failures += 1

    scope = ', '.join(p.name for p in targets)
    if failures:
        print(f'\n{failures} broken link(s) across {checked} relative links in root Markdown.')
        print('Fix the links above, or update the heading they point at.')
        return 1

    print(f'OK: {checked} relative links resolve across {len(targets)} root Markdown files.')
    print(f'scope: {scope}')
    print('docs/** anchors are gated separately by mkdocs build --strict '
          '(validation.links.anchors).')
    return 0


if __name__ == '__main__':
    root = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
    sys.exit(check(root))
