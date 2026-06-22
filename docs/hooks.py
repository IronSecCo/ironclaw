"""MkDocs build hooks for the IronClaw docs site.

Two jobs, both reproducible and run identically in local builds and in CI:

1. Copy the canonical OpenAPI spec (``api/openapi.yaml``, the single source of
   truth) into ``docs/reference/openapi.yaml`` so the API reference page renders
   a same-origin, vendored copy — no committed duplicate, no runtime fetch from
   an external host.
2. Stamp the site with the release version derived the same way the release
   pipeline derives it (``v0.1.<commit-count>``), so every published site is
   tied to the source commit it was built from. CI passes the version via
   ``IRONCLAW_DOCS_VERSION``; locally we fall back to ``git rev-list --count``.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

# repo_root/docs/hooks.py -> repo_root
REPO_ROOT = Path(__file__).resolve().parent.parent
BASE_VERSION = "0.1"


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


def on_config(config, **kwargs):
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
