"""MkDocs build hooks for the IronClaw docs site.

Three jobs, all reproducible and run identically in local builds and in CI:

1. Assemble the site ``nav`` from per-directory ``.nav.yml`` fragment files
   instead of one shared ``nav:`` block in ``mkdocs.yml``. A single shared nav
   line is a merge-collision magnet: every docs/provider PR edits it, so merging
   one flips all the siblings to CONFLICTING and forces serial rebase-then-merge.
   Splitting the nav into per-directory fragments means N docs PRs merge in any
   order without touching a shared line. See ``_assemble_nav`` for the format.
2. Copy the canonical OpenAPI spec (``api/openapi.yaml``, the single source of
   truth) into ``docs/reference/openapi.yaml`` so the API reference page renders
   a same-origin, vendored copy — no committed duplicate, no runtime fetch from
   an external host.
3. Stamp the site with the release version derived the same way the release
   pipeline derives it (``v0.1.<commit-count>``), so every published site is
   tied to the source commit it was built from. CI passes the version via
   ``IRONCLAW_DOCS_VERSION``; locally we fall back to ``git rev-list --count``.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

import yaml

# repo_root/docs/hooks.py -> repo_root
REPO_ROOT = Path(__file__).resolve().parent.parent
DOCS_DIR = REPO_ROOT / "docs"
BASE_VERSION = "0.1"

# The per-directory nav fragment filename. A dotfile on purpose: MkDocs ignores
# dotfiles when collecting the doc tree, so these fragments never leak into the
# built site, while this hook reads them straight off disk.
NAV_FILE = ".nav.yml"


def _derive_version() -> str:
    env = os.environ.get("IRONCLAW_DOCS_VERSION")
    if env:
        return env if env.startswith("v") else f"v{env}"
    try:
        count = subprocess.check_output(
            ["git", "rev-list", "--count", "HEAD"],
            cwd=REPO_ROOT,
            stderr=subprocess.DEVNULL,
        ).decode().strip()
        return f"v{BASE_VERSION}.{count}"
    except Exception:
        return f"v{BASE_VERSION}.dev"


class NavError(Exception):
    """Raised when a nav fragment is missing, malformed, or points at a file
    that does not exist. We fail the build loudly rather than let MkDocs fall
    back to an auto-generated nav that silently drops or reorders pages."""


def _read_fragment(frag_path: Path) -> list:
    """Load a ``.nav.yml`` fragment and return its ``nav:`` list."""
    if not frag_path.exists():
        raise NavError(f"nav fragment not found: {frag_path.relative_to(REPO_ROOT)}")
    data = yaml.safe_load(frag_path.read_text(encoding="utf-8")) or {}
    if not isinstance(data, dict) or "nav" not in data:
        raise NavError(
            f"nav fragment {frag_path.relative_to(REPO_ROOT)} must contain a top-level 'nav:' list"
        )
    nav = data["nav"]
    if not isinstance(nav, list):
        raise NavError(
            f"'nav:' in {frag_path.relative_to(REPO_ROOT)} must be a list, got {type(nav).__name__}"
        )
    return nav


def _rel(path: Path) -> str:
    """docs-dir-relative POSIX path, the form MkDocs expects in nav."""
    return path.relative_to(DOCS_DIR).as_posix()


def _check_page(base: Path, filename: str) -> str:
    page = (base / filename).resolve()
    if not page.exists():
        raise NavError(
            f"nav references missing page: {_rel(base)}/{filename}".lstrip("/")
        )
    return _rel(page)


def _resolve_items(items: list, base: Path) -> list:
    """Resolve one fragment's item list into a MkDocs nav list.

    ``base`` is the directory the fragment lives in; every relative path in the
    fragment is resolved against it. Supported item forms:

      - ``Title: page.md``      -> a page (path relative to this fragment's dir)
      - ``Title: subdir``       -> a section whose children come from
                                    ``subdir/.nav.yml`` (title from the key)
      - ``Title: [ ... ]``      -> an inline nested section
      - ``"*.md"`` (any glob)   -> every matching file in this fragment's dir not
                                    already listed above, sorted, auto-titled by
                                    MkDocs from each page's H1 (zero-edit adds)
      - ``page.md``             -> a page, auto-titled
    """
    resolved: list = []
    listed: set[str] = set()  # filenames explicitly named in this fragment's dir

    for item in items:
        if isinstance(item, str):
            if any(ch in item for ch in "*?[") :
                # Glob: expand later, once we know everything explicitly listed.
                resolved.append(("__glob__", item))
                continue
            resolved.append(_check_page(base, item))
            listed.add(item)
            continue

        if not isinstance(item, dict) or len(item) != 1:
            raise NavError(
                f"nav item in {_rel(base) or 'docs'} must be a single-key mapping or string, got: {item!r}"
            )

        (title, value), = item.items()

        if isinstance(value, list):
            resolved.append({title: _resolve_items(value, base)})
        elif isinstance(value, str) and value.endswith(".md"):
            resolved.append({title: _check_page(base, value)})
            listed.add(value)
        elif isinstance(value, str):
            # Directory reference: pull the subdirectory's own fragment.
            subdir = (base / value).resolve()
            if not subdir.is_dir():
                raise NavError(
                    f"nav references '{value}' as a directory under {_rel(base) or 'docs'}, "
                    "but it is not a directory"
                )
            children = _resolve_items(_read_fragment(subdir / NAV_FILE), subdir)
            resolved.append({title: children})
        else:
            raise NavError(
                f"nav item {title!r} in {_rel(base) or 'docs'} has unsupported value: {value!r}"
            )

    # Expand any glob tokens against files not already named in this fragment.
    out: list = []
    for entry in resolved:
        if isinstance(entry, tuple) and entry[0] == "__glob__":
            pattern = entry[1]
            for match in sorted(base.glob(pattern)):
                if match.name.startswith(".") or match.name in listed:
                    continue
                out.append(_rel(match))  # bare path -> MkDocs titles it from the H1
        else:
            out.append(entry)
    return out


def _assemble_nav(config) -> None:
    """Build ``config['nav']`` from the per-directory ``.nav.yml`` fragments.

    Canonical source of the site nav is ``docs/.nav.yml`` (the root fragment),
    which references churn-heavy directories (providers/, tutorials/,
    integrations/) by name so they resolve to their own fragments. Adding a page
    to one of those directories touches only that directory's fragment — never a
    line shared with unrelated docs PRs.
    """
    root = _read_fragment(DOCS_DIR / NAV_FILE)
    nav = _resolve_items(root, DOCS_DIR)
    if not nav:
        raise NavError("assembled nav is empty — refusing to build with no navigation")
    config["nav"] = nav


def on_config(config, **kwargs):
    _assemble_nav(config)

    version = _derive_version()
    # Surfaced in the footer copyright and available to templates via extra.version.
    config.setdefault("extra", {})
    config["extra"]["version"] = version
    base = config.get("copyright") or ""
    sep = " · " if base else ""
    config["copyright"] = f"{base}{sep}Docs built from {version}"
    return config


def on_pre_build(config, **kwargs):
    src = REPO_ROOT / "api" / "openapi.yaml"
    dst = REPO_ROOT / "docs" / "reference" / "openapi.yaml"
    if not src.exists():
        raise FileNotFoundError(
            f"OpenAPI spec not found at {src}; the API reference page cannot render."
        )
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(src, dst)
