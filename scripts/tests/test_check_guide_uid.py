#!/usr/bin/env python3
"""Tests for scripts/check-guide-uid.py (IRO-699).

This checker's passing output is "exit 0", which is also what a checker that
enumerates nothing produces, so the tests below are negative controls: each
builds a tree carrying a real defect and asserts the checker names it, or
builds a degenerate tree and asserts the checker refuses to call it green.

Three specific defects are pinned, all of them real:

* the IRO-698 shape, a bullet and a run block in one guide pinning different
  uids, which shipped to `main` in PR #643;
* the IRO-699 shape, a uid attributed to `--fix` that `--fix` does not emit;
* the pgadmin4 shape, a pasted "after" scan block naming a uid that nothing in
  the guide sets.

Equally important is what must NOT fire. haproxy, grafana and prometheus
already PASS non-root by default, so their `--user` pins the image's own uid
rather than quoting `--fix`. A checker that "fixed" those would replace a true
statement with a false one, so test_image_own_uid_is_not_flagged holds that
line.

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

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check-guide-uid.py'
REPO_ROOT = SCRIPT.parent.parent

UID = '65532:65532'


def load_module():
    spec = importlib.util.spec_from_file_location('check_guide_uid', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root: pathlib.Path):
    """Run the checker against `root`, returning (exit code, stderr+stdout)."""
    module = load_module()
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = module.main(['check-guide-uid.py', str(root)])
    return code, out.getvalue() + err.getvalue()


def guide(
    *,
    default_verdict='FAIL',
    default_detail='runs as root (uid 0); a container escape starts with host-uid 0',
    bullet_uid=UID,
    run_uid=UID,
    after_detail=None,
):
    """Render a guide with the same shape as the real ones.

    `run_uid=None` omits the `--user` line from the hardened run block, which is
    how pgadmin4 is actually written. `after_detail=None` omits the pasted scan
    output, which most guides do not carry.
    """
    bullet = (
        f'- **`--user {bullet_uid}`** (Non-root user, +15): pin a non-root uid.\n'
        if bullet_uid is not None
        else ''
    )
    user_line = f'  --user {run_uid} \\\n' if run_uid is not None else ''
    after = (
        '\n```\nscore:   89/100  grade B  (solid, minor gaps)\n'
        f'Non-root user (uid != 0)    [+] PASS  15/15  {after_detail}\n```\n'
        if after_detail is not None
        else ''
    )
    return (
        '# How to harden a thing\n\n'
        '| Dimension | Verdict | Score | What the scan found |\n'
        '|-----------|:-------:|------:|---------------------|\n'
        f'| Non-root user (uid != 0) | x {default_verdict} | 0/15 | {default_detail} |\n'
        '\n## Harden it: the exact `--fix` remediation\n\n'
        f'{bullet}'
        '\n## Before and after\n\n'
        '```bash\n'
        '# Before: 63/100, grade C\n'
        'docker run -d --name thing thing:1\n'
        '\n# After: 100/100, grade A\n'
        'docker run -d --name thing-hardened \\\n'
        f'{user_line}'
        '  --cap-drop=ALL \\\n'
        '  thing:1\n'
        '```\n'
        f'{after}'
    )


# A correct guide carrying both of the shapes the checker asserts on: a
# remediated `--user` and a pasted "after" block. Most fixtures include it so
# the run is non-vacuous, leaving the guide under test as the only variable.
# Tests that are specifically about the vacuity gate opt out with baseline=False.
BASELINE = 'harden-baseline-container-isolation.md'


def build_tree(
    root: pathlib.Path,
    guides: dict,
    hardened_uid: str = UID,
    baseline: bool = True,
):
    """Write a minimal repo: docs/blog guides plus the Go file holding the uid."""
    blog = root / 'docs' / 'blog'
    blog.mkdir(parents=True)
    if baseline:
        (blog / BASELINE).write_text(
            guide(
                bullet_uid=hardened_uid,
                run_uid=hardened_uid,
                after_detail=f'runs as {hardened_uid} (uid != 0)',
            ),
            encoding='utf-8',
        )
    for name, text in guides.items():
        (blog / name).write_text(text, encoding='utf-8')
    go = root / 'internal' / 'host' / 'scan'
    go.mkdir(parents=True)
    (go / 'remediate.go').write_text(
        'package scan\n\n'
        f'const hardenedUID = "{hardened_uid}"\n',
        encoding='utf-8',
    )
    return root


class CheckGuideUidTest(unittest.TestCase):
    @contextlib.contextmanager
    def tree(self, guides, **kwargs):
        with tempfile.TemporaryDirectory() as tmp:
            yield build_tree(pathlib.Path(tmp), guides, **kwargs)

    def test_clean_tree_passes_and_names_its_counts(self):
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                after_detail=f'runs as {UID} (uid != 0)')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 0, output)
        # A green that cannot distinguish "held" from "checked nothing" is the
        # bug these two lines exist to prevent: two guides, each with a bullet
        # and a run-block flag, each with one pasted output row.
        self.assertIn('4 `--user` flag(s) match', output)
        self.assertIn('2 pasted scan-output row(s)', output)

    def test_wrong_uid_attributed_to_fix_is_caught(self):
        """The IRO-699 defect: a uid `--fix` does not emit, under its heading."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                bullet_uid='1000:1000', run_uid='1000:1000')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('--user 1000:1000', output)
        self.assertIn('--fix emits `--user 65532:65532`', output)

    def test_invented_image_uid_is_caught(self):
        """couchdb quoted 5984, its own port number, as `--fix` output."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                bullet_uid='5984:5984', run_uid='5984:5984')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('--user 5984:5984', output)

    def test_bullet_and_run_block_disagreeing_is_caught(self):
        """The IRO-698 defect that shipped in PR #643: half-applied fix."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                bullet_uid=UID, run_uid='1000:1000')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('--user 1000:1000', output)

    def test_after_scan_disagreeing_with_run_block_is_caught(self):
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                after_detail='runs as 1000:1000 (uid != 0)')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('runs as 1000:1000', output)
        self.assertIn(f'sets `--user {UID}`', output)

    def test_after_scan_uid_nothing_sets_is_caught(self):
        """The pgadmin4 defect: PASS by default, no `--user`, yet claims one."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                default_verdict='PASS',
                default_detail='runs as pgadmin (uid != 0)',
                bullet_uid=None,
                run_uid=None,
                after_detail='runs as 1000:1000 (uid != 0)')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('runs as 1000:1000', output)
        self.assertIn('sets no `--user`', output)

    def test_image_own_uid_is_not_flagged(self):
        """haproxy/grafana/prometheus: already non-root, so 472 is not a lie.

        Rewriting these to the hardened uid would break the container, whose
        data volume the image's own uid owns. The checker must leave them be.
        """
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                default_verdict='PASS',
                default_detail='runs as 472 (uid != 0)',
                bullet_uid=None,
                run_uid='472:472',
                after_detail='runs as 472:472 (uid != 0)')}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 0, output)

    def test_uid_is_read_from_go_not_hardcoded(self):
        """Move the constant and the assertion must move with it."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                bullet_uid='4242:4242', run_uid='4242:4242')},
            hardened_uid='4242:4242',
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 0, output)
        # And the old value is now the failure, not the pass.
        with self.tree(
            {'harden-a-container-isolation.md': guide()},
            hardened_uid='4242:4242',
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('4242:4242', output)

    def test_missing_go_constant_is_an_error_not_a_pass(self):
        with self.tree({'harden-a-container-isolation.md': guide()}) as root:
            (root / 'internal' / 'host' / 'scan' / 'remediate.go').write_text(
                'package scan\n', encoding='utf-8'
            )
            code, output = run(root)
        self.assertEqual(code, 2, output)
        self.assertIn('hardenedUID', output)

    def test_no_guides_is_an_error_not_a_pass(self):
        with self.tree({}, baseline=False) as root:
            code, output = run(root)
        self.assertEqual(code, 2, output)
        self.assertIn('vacuously', output)

    def test_guide_without_a_scan_table_is_caught(self):
        """A guide whose table drifted is unguarded, so it fails loudly."""
        with self.tree(
            {'harden-a-container-isolation.md': '# no table here\n'}
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 1, output)
        self.assertIn('default-scan table', output)

    def test_parser_drift_is_an_error_not_a_green(self):
        """Every guide PASSing by default means rule 1 asserted nothing."""
        with self.tree(
            {'harden-a-container-isolation.md': guide(
                default_verdict='PASS',
                default_detail='runs as nobody (uid != 0)',
                bullet_uid=None,
                run_uid=None)},
            baseline=False,
        ) as root:
            code, output = run(root)
        self.assertEqual(code, 2, output)
        self.assertIn('asserting nothing', output)

    def test_real_repo_is_clean(self):
        """The committed tree must satisfy the checker it ships with."""
        code, output = run(REPO_ROOT)
        self.assertEqual(code, 0, output)


if __name__ == '__main__':
    unittest.main()
