#!/usr/bin/env python3
"""Tests for scripts/check-md-fences.py (IRO-707).

This checker's passing output is "exit 0", which is also what a checker that
enumerates nothing produces. So most of what follows are negative controls: each
builds a tree with a real fence defect and asserts the checker names the
file:line, or builds a degenerate tree and asserts the checker refuses to call
it green.

The specific defect being pinned is the one IRO-707 was filed for: PR #656
deleted the opening and closing fence around the sample `ironctl scan --md`
output in docs/blog/audit-your-sandbox-in-10-seconds.md, leaving the file
perfectly *balanced* while the literal sample text started rendering as a real
heading and a real table. All 17 checks passed. The exact pre-fix byte pattern
is reproduced in test_deleted_fence_pair_is_caught -- a balance-only guard
passes on it, which is why the stray-blank-line rule exists.

The bare-opening-fence rule (IRO-709) landed later, once #656 had tagged the
corpus it would otherwise have failed on. Its controls are in
BareOpeningFenceTest.

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

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check-md-fences.py'
REPO_ROOT = SCRIPT.parent.parent


def load_module():
    spec = importlib.util.spec_from_file_location('check_md_fences', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root: pathlib.Path):
    """Run the checker against `root`, returning (exit code, stderr+stdout)."""
    module = load_module()
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = module.main(['check-md-fences.py', str(root)])
    return code, out.getvalue() + err.getvalue()


def write(root: pathlib.Path, rel: str, body: str):
    """Write one markdown file under `root`, creating parent directories."""
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding='utf-8')
    return path


def build_tree(root: pathlib.Path, body: str, rel: str = 'docs/page.md'):
    """A minimal repo root: a docs/ directory holding one markdown file."""
    (root / 'docs').mkdir(parents=True, exist_ok=True)
    write(root, rel, body)


# The bytes PR #656 produced, reduced to the two lines it damaged. Note that the
# fence count is even and every remaining block closes: this is what a
# balance-only checker sees as clean.
DELETED_FENCE_PAIR = """# Audit your sandbox

Prefer a table? `--md` prints a shareable markdown block:

```bash
ironctl scan my-sandbox --md
```


### IronClaw containment scan: `my-sandbox` scored **100/100 (grade A)**

| Dimension | Verdict | Score |
| --- | --- | --- |
| Runs as non-root | PASS | 15/15 |


## Wire it into CI
"""

# The same page with the two fences restored, exactly as the fix to #656 does.
INTACT_FENCE_PAIR = DELETED_FENCE_PAIR.replace(
    '```\n\n\n### IronClaw', '```\n\n```text\n### IronClaw'
).replace('| PASS | 15/15 |\n\n\n## Wire', '| PASS | 15/15 |\n```\n\n## Wire')


class DeletedFenceTest(unittest.TestCase):
    """The IRO-707 defect itself: a *balanced* file with two fences removed."""

    def test_deleted_fence_pair_is_caught(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, DELETED_FENCE_PAIR, 'docs/blog/audit.md')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        # Both damaged sites, each named with its own line number.
        self.assertIn('docs/blog/audit.md:9:', output)
        self.assertIn('docs/blog/audit.md:16:', output)

    def test_balance_alone_would_not_catch_it(self):
        """Pins *why* the second rule exists, not just that it fires.

        If this ever fails, the fixture stopped reproducing #656 and the test
        above would be passing for the wrong reason.
        """
        module = load_module()
        opens = 0
        for line in DELETED_FENCE_PAIR.split('\n'):
            if module.FENCE.match(line):
                opens += 1
        self.assertEqual(opens % 2, 0, 'fixture must be fence-balanced')

    def test_restoring_the_fences_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, INTACT_FENCE_PAIR, 'docs/blog/audit.md')
            code, output = run(root)
        self.assertEqual(code, 0, output)


class UnclosedFenceTest(unittest.TestCase):
    def test_unclosed_fence_is_caught_at_the_opening_line(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```bash\nls\n\nand then some prose.\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('docs/page.md:3:', output)
        self.assertIn('unclosed code fence', output)

    def test_tagged_closing_fence_is_caught(self):
        """A "standardize the fences" edit that tags the *closing* fence too.

        ```text ... ```text reads as two openings, so the block never closes.
        This is the sibling mistake to the one #656 made.
        """
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```text\nout\n```text\n\ndone.\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('unclosed code fence', output)

    def test_tilde_fence_closes_a_tilde_fence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n~~~text\nout\n~~~\n\ndone.\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_longer_fence_nests_shorter_ones(self):
        """A ````markdown block quoting ``` fences must not be misread."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(
                root, '# t\n\n````markdown\n```bash\nls\n```\n````\n\ndone.\n'
            )
            code, output = run(root)
        self.assertEqual(code, 0, output)


class FalsePositiveTest(unittest.TestCase):
    """The shapes a noisy version of this check would flag. All are legal."""

    def test_blank_lines_inside_a_fence_are_content(self):
        """Go and Python both put double blanks between declarations."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```go\nfunc a() {}\n\n\nfunc b() {}\n```\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_inline_triple_backtick_span_is_not_a_fence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\nWrite ```` ``` ```` to open a block.\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_single_trailing_newline_is_not_a_stray_blank(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\nprose.\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_code_of_conduct_is_excluded(self):
        """Verbatim Contributor Covenant text, seven double-blanks of its own."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\nprose.\n')
            write(root, 'CODE_OF_CONDUCT.md', '# CoC\n\n\n## Scope\n\n\ntext.\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)


class BareOpeningFenceTest(unittest.TestCase):
    """Rule 3 (IRO-709). Held back from IRO-707 until #656 tagged the corpus.

    The negative control and its inverse are deliberately adjacent: the rule is
    only worth anything if it fires on an untagged block *and* stays quiet on a
    tagged one. The remaining cases pin the two shapes a naive version of this
    rule gets wrong -- a bare closing fence, which is required to be bare, and a
    bare fence quoted inside a longer block, which is content.
    """

    def test_bare_opening_fence_is_flagged(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```\nsome output\n```\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('docs/page.md:3:', output)
        self.assertIn('bare opening code fence', output)

    def test_tagged_opening_fence_is_green(self):
        """The inverse control: the rule must not fire on the fixed shape."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```text\nsome output\n```\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_closing_fence_is_not_reported_as_bare(self):
        """A closing fence takes no info string, so it must never be a finding.

        Asserts the count, not just the exit code: a rule that fired on both
        ends of every block would still exit 1 on the test above.
        """
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n```\nout\n```\n\n```\nmore\n```\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertEqual(output.count('bare opening code fence'), 2, output)
        self.assertIn('docs/page.md:3:', output)
        self.assertIn('docs/page.md:7:', output)

    def test_bare_fence_quoted_inside_a_longer_block_is_content(self):
        """A ``` inside a ````markdown block is sample text, not an opening."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n````markdown\n```\nls\n```\n````\n')
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_bare_tilde_fence_is_flagged(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\n~~~\nout\n~~~\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('bare opening code fence', output)


class ScopeTest(unittest.TestCase):
    def test_root_readme_is_in_scope(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\nprose.\n')
            write(root, 'README.md', '# r\n\n```bash\nls\n\nprose.\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('README.md:3:', output)

    def test_mdx_under_docs_is_in_scope(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            build_tree(root, '# t\n\nprose.\n')
            write(root, 'docs/site/quickstart.mdx', '# q\n\n\nprose.\n')
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('docs/site/quickstart.mdx:3:', output)

    def test_empty_docs_tree_is_not_green(self):
        """A glob that matches nothing must not report "all fences balanced"."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'docs').mkdir()
            code, output = run(root)
        self.assertEqual(code, 2, output)
        self.assertIn('vacuously', output)

    def test_missing_docs_dir_is_a_usage_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, output = run(pathlib.Path(tmp))
        self.assertEqual(code, 2, output)


class RealRepoTest(unittest.TestCase):
    def test_repo_is_clean(self):
        """Acceptance criterion 1: the guard passes on the tree it ships in."""
        code, output = run(REPO_ROOT)
        self.assertEqual(code, 0, output)


if __name__ == '__main__':
    unittest.main()
