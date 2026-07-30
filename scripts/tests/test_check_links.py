#!/usr/bin/env python3
"""Tests for scripts/check_links.py (IRO-688).

A link checker is the kind of tool that passes green whether or not it works: the
happy path is "no output, exit 0", which is also exactly what a checker that
parses nothing at all produces. #635 landed one that was inert (referenced by no
workflow) and, once run, wrong in three ways. So every test below is a negative
control: it feeds the checker a link that IS broken and asserts it says so, or
feeds it a construct that is NOT a link and asserts it stays quiet.

The three defects being pinned:

  1. Fenced code blocks were parsed for headings, so `# --addr defaults to ...`
     in a bash snippet registered as an anchor. That is a false NEGATIVE: a link
     to #addr-defaults-to- passed while the anchor did not exist.
  2. HTML comments were parsed for links, so the commented-out card-contract
     template in docs/scan-coverage.md registered as a link to `<deep doc>`.
     That false positive was suppressed with a hardcoded skip for that exact
     filename and URL, which is what test_no_special_cases asserts is gone.
  3. Slugs were computed with neither renderer's rules. Root Markdown renders on
     GitHub, and github-slugger keeps hyphen runs where python-markdown collapses
     them, so `the exact `--fix`` is #the-exact---fix on GitHub and
     #the-exact-fix in MkDocs. test_github_slug_semantics pins the GitHub form.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import pathlib
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check_links.py'
REPO_ROOT = SCRIPT.parent.parent


def load_module():
    spec = importlib.util.spec_from_file_location('check_links', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    sys.modules['check_links'] = module
    spec.loader.exec_module(module)
    return module


check_links = load_module()


def run_on(files: dict[str, str]) -> tuple[int, str]:
    """Write ``files`` into a temp repo and run the checker over it."""
    with tempfile.TemporaryDirectory() as tmp:
        root = pathlib.Path(tmp)
        for name, text in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding='utf-8')
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            code = check_links.check(root)
        return code, buf.getvalue()


class TestSlug(unittest.TestCase):
    def test_github_slug_semantics(self):
        """github-slugger keeps hyphen runs; python-markdown collapses them."""
        self.assertEqual(
            check_links.github_slug('Harden it: the exact `--fix` remediation'),
            'harden-it-the-exact---fix-remediation',
        )
        self.assertEqual(check_links.github_slug('Project status'), 'project-status')
        self.assertEqual(check_links.github_slug('Windows (via WSL2)'), 'windows-via-wsl2')
        self.assertEqual(check_links.github_slug('8. What counts as a vulnerability?'),
                         '8-what-counts-as-a-vulnerability')
        # Underscores survive, emoji do not.
        self.assertEqual(check_links.github_slug('A_b 🔒 c'), 'a_b--c')

    def test_matches_real_github_anchors(self):
        """Fixtures captured from github.com's own rendering of this repo.

        Verified 2026-07-30 by scraping the `id="user-content-..."` anchors off
        the rendered blob pages for README.md, CONTRIBUTING.md, SECURITY.md and
        LICENSING.md: all 69 anchors matched github_slug() exactly. These four
        are the interesting ones -- non-ASCII punctuation (em dash, arrow) that
        an ASCII-only character class silently keeps. Re-verify with:

            curl -sL https://github.com/IronSecCo/ironclaw/blob/main/README.md \\
              | grep -o 'id=\\\\"user-content-[^"\\\\]*'
        """
        cases = {
            'Quickstart — your first PR in 5 minutes': 'quickstart--your-first-pr-in-5-minutes',
            '`ironclaw-controlplane` — the host daemon': 'ironclaw-controlplane--the-host-daemon',
            'Homebrew (macOS + Linux)': 'homebrew-macos--linux',
            'Run a 100% local model (Ollama, LM Studio, vLLM) — no cloud key':
                'run-a-100-local-model-ollama-lm-studio-vllm--no-cloud-key',
        }
        for heading, expected in cases.items():
            with self.subTest(heading=heading):
                self.assertEqual(check_links.github_slug(heading), expected)

    @unittest.skipUnless(importlib.util.find_spec('markdown'), 'python-markdown not installed')
    def test_diverges_from_python_markdown_on_purpose(self):
        """Pin the disagreement, so nobody "fixes" this to the MkDocs rules."""
        from markdown.extensions.toc import slugify as md_slugify
        heading = 'Harden it: the exact --fix remediation'
        self.assertNotEqual(check_links.github_slug(heading), md_slugify(heading, '-'))


class TestNegativeControls(unittest.TestCase):
    def test_missing_file_is_caught(self):
        code, out = run_on({'README.md': 'see [the guide](docs/nope.md)\n'})
        self.assertEqual(code, 1)
        self.assertIn('file missing', out)
        self.assertIn('README.md:1', out)

    def test_broken_same_file_anchor_is_caught(self):
        code, out = run_on({'README.md': '# Real Heading\n\n[jump](#not-a-heading)\n'})
        self.assertEqual(code, 1)
        self.assertIn('anchor broken', out)
        self.assertIn('#not-a-heading', out)

    def test_valid_same_file_anchor_passes(self):
        code, out = run_on({'README.md': '# Real Heading\n\n[jump](#real-heading)\n'})
        self.assertEqual(code, 0, out)

    def test_broken_cross_file_anchor_is_caught(self):
        code, out = run_on({
            'SECURITY.md': '[policy](docs/threat-model.md#section-9)\n',
            'docs/threat-model.md': '## 8. What counts as a vulnerability?\n',
        })
        self.assertEqual(code, 1)
        self.assertIn('anchor broken', out)

    def test_valid_cross_file_anchor_passes(self):
        code, out = run_on({
            'SECURITY.md': '[policy](docs/threat-model.md#8-what-counts-as-a-vulnerability)\n',
            'docs/threat-model.md': '## 8. What counts as a vulnerability?\n',
        })
        self.assertEqual(code, 0, out)

    def test_hyphen_run_anchor_resolves(self):
        """The case the old slugify got wrong in the passing direction."""
        code, out = run_on({
            'README.md': '[fix](docs/h.md#harden-it-the-exact---fix-remediation)\n',
            'docs/h.md': '## Harden it: the exact `--fix` remediation\n',
        })
        self.assertEqual(code, 0, out)

    def test_reference_and_href_links_are_checked(self):
        code, out = run_on({'README.md': '[ref]: docs/gone.md\n\n<a href="docs/gone2.md">x</a>\n'})
        self.assertEqual(code, 1)
        self.assertIn('reference link', out)
        self.assertIn('HTML href', out)


class TestStripping(unittest.TestCase):
    def test_heading_inside_fence_is_not_an_anchor(self):
        """Defect 1: a shell comment in a code fence must not mint an anchor."""
        code, out = run_on({
            'README.md': '# Title\n\n```bash\n# --addr defaults to http://127.0.0.1:8787\n```\n\n'
                         '[x](#-addr-defaults-to-http1270018787)\n',
        })
        self.assertEqual(code, 1, out)
        self.assertIn('anchor broken', out)

    def test_link_inside_fence_is_ignored(self):
        code, out = run_on({'README.md': '```\n[not a link](does/not/exist.md)\n```\n'})
        self.assertEqual(code, 0, out)

    def test_link_inside_html_comment_is_ignored(self):
        """Defect 2: this is what removed the need for the <deep doc> skip."""
        code, out = run_on({
            'README.md': '<!--\n  template:\n  [:octicons-arrow-right-24: <label>](<deep doc>)\n-->\n',
        })
        self.assertEqual(code, 0, out)

    def test_heading_inside_html_comment_is_not_an_anchor(self):
        code, out = run_on({'README.md': '<!-- # Ghost Heading -->\n\n[x](#ghost-heading)\n'})
        self.assertEqual(code, 1, out)
        self.assertIn('anchor broken', out)


class TestRepoState(unittest.TestCase):
    def test_green_on_this_repo(self):
        """The gate must be green on main, or it gets disabled within a day."""
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            code = check_links.check(REPO_ROOT)
        self.assertEqual(code, 0, buf.getvalue())

    def test_no_special_cases(self):
        """IRO-688 task 3: the hardcoded scan-coverage.md skip must be gone."""
        source = SCRIPT.read_text(encoding='utf-8')
        self.assertNotIn("'<deep doc>'", source)
        self.assertNotIn('"<deep doc>"', source)
        self.assertNotIn("== 'scan-coverage.md'", source)

    def test_docs_tree_is_not_scanned(self):
        """docs/** belongs to mkdocs --strict; two slug algorithms is a trap."""
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            check_links.check(REPO_ROOT)
        self.assertIn('root Markdown files', buf.getvalue())
        self.assertNotIn('scan-coverage.md', buf.getvalue())


if __name__ == '__main__':
    unittest.main()
