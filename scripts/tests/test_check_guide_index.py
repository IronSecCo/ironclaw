#!/usr/bin/env python3
"""Tests for scripts/check-guide-index.py (IRO-696).

This checker's passing output is "exit 0", which is also what a checker that
enumerates nothing produces. So the tests below are negative controls: each one
builds a tree where a guide IS missing from a surface and asserts the checker
says so, or builds a degenerate tree and asserts the checker refuses to call it
green.

The specific defect being pinned is the one IRO-696 found on `main`: a guide can
be present on disk, in the nav, and on the hub, yet absent from an index, and
nothing in CI notices, because a missing entry is not a broken link. The real
pre-fix repo state is reproduced in test_missing_from_hub_is_caught.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check-guide-index.py'
REPO_ROOT = SCRIPT.parent.parent


def load_module():
    spec = importlib.util.spec_from_file_location('check_guide_index', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root: pathlib.Path):
    """Run the checker against `root`, returning (exit code, stderr+stdout)."""
    module = load_module()
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = module.main(['check-guide-index.py', str(root)])
    return code, out.getvalue() + err.getvalue()


def build_tree(root: pathlib.Path, guides, in_nav=None, in_hub=None):
    """Write a minimal docs/blog tree.

    `guides` land on disk; `in_nav` / `in_hub` default to all of them, so a test
    creates a gap by passing a subset.
    """
    blog = root / 'docs' / 'blog'
    blog.mkdir(parents=True)
    for name in guides:
        (blog / name).write_text('# guide\n', encoding='utf-8')
    nav = guides if in_nav is None else in_nav
    hub = guides if in_hub is None else in_hub
    (blog / '.nav.yml').write_text(
        ''.join(f'  - "A guide": {n}\n' for n in nav), encoding='utf-8'
    )
    (blog / 'hardening-guides.md').write_text(
        ''.join(f'| [X]({n}) | 48/100 D | 100/100 A | root |\n' for n in hub),
        encoding='utf-8',
    )
    return root


class CheckGuideIndexTest(unittest.TestCase):
    def test_fully_wired_tree_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = build_tree(pathlib.Path(tmp), ['harden-a-container-isolation.md',
                                                  'harden-b-container-isolation.md'])
            code, output = run(root)
            self.assertEqual(code, 0, output)
            # An honest green names the count it enumerated, so "all wired" and
            # "checked nothing" cannot read the same in a CI log.
            self.assertIn('2 hardening guides', output)

    def test_missing_from_nav_is_caught(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = build_tree(
                pathlib.Path(tmp),
                ['harden-a-container-isolation.md', 'harden-b-container-isolation.md'],
                in_nav=['harden-a-container-isolation.md'],
            )
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('harden-b-container-isolation.md', output)
            self.assertIn('.nav.yml', output)

    def test_missing_from_hub_is_caught(self):
        """The IRO-696 shape: on disk and in the nav, absent from an index."""
        with tempfile.TemporaryDirectory() as tmp:
            root = build_tree(
                pathlib.Path(tmp),
                ['harden-a-container-isolation.md', 'harden-haproxy-container-isolation.md'],
                in_hub=['harden-a-container-isolation.md'],
            )
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('harden-haproxy-container-isolation.md', output)
            self.assertIn('hardening-guides.md', output)

    def test_no_guides_is_not_a_pass(self):
        """A glob matching nothing must fail, not report a vacuous all-clear."""
        with tempfile.TemporaryDirectory() as tmp:
            root = build_tree(pathlib.Path(tmp), [])
            code, output = run(root)
            self.assertEqual(code, 2, output)
            self.assertIn('vacuous', output)

    def test_missing_blog_dir_is_not_a_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, output = run(pathlib.Path(tmp))
            self.assertEqual(code, 2, output)

    def test_real_repo_is_wired(self):
        """The committed tree must satisfy the check it ships."""
        code, output = run(REPO_ROOT)
        self.assertEqual(code, 0, output)


if __name__ == '__main__':
    unittest.main()
