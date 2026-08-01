#!/usr/bin/env python3
"""Tests for scripts/check-guide-scores.py (IRO-734).

The defect this checker exists to catch shipped through 17 green checks, so its
own green is worth nothing until proven non-vacuous. Every test below is a
negative control: build a tree carrying a real defect and assert the checker
names it, or build a degenerate tree and assert it refuses to call it green.

The headline control is `test_pr661_haproxy_rewrite`, which rebuilds the exact
five defects PR #661 shipped past CI and took four rounds of manual review to
find:

* the scores column summed to 58 while four sites claimed 63/100;
* an invented `Privilege escalation` dimension the scorer does not have;
* the canonical `No docker.sock exposure` row deleted;
* `Read-only root filesystem` moved from its real 0/10 to 5/15;
* `Dropped capabilities` reported WARN where the scorer emits FAIL at 4/20.

The camouflage was that the denominators still summed to 100, so a checker that
only added up the `max` column would have been green on all five.

Equally important is what must NOT fire: `test_real_corpus_is_clean` runs the
checker against the repository's own 56 guides, and `test_canonical_set_matches`
asserts the parsed dimension set is the real seven-dimension one rather than an
empty set that would make every table trivially conformant.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import pathlib
import shutil
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check-guide-scores.py'
REPO_ROOT = SCRIPT.parent.parent

CANONICAL_ROWS = [
    ('Non-root user (uid != 0)', '✅ PASS', 15, 15, 'runs as haproxy (uid != 0)'),
    ('Dropped capabilities', '❌ FAIL', 4, 20, 'default capability set retained'),
    ('Seccomp profile', '✅ PASS', 15, 15, 'seccomp profile active'),
    ('Network isolation / egress', '⚠️ WARN', 4, 15, 'network=bridge: outbound egress is possible'),
    ('Read-only root filesystem', '❌ FAIL', 0, 10, 'root filesystem is writable'),
    ('No docker.sock exposure', '✅ PASS', 15, 15, 'no control socket mounted'),
    ('No shared host namespaces', '✅ PASS', 10, 10, 'no host PID/IPC/network sharing'),
]


def load_module():
    spec = importlib.util.spec_from_file_location('check_guide_scores', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root: pathlib.Path, *extra: str):
    """Run the checker against `root`, returning (exit code, stdout+stderr)."""
    module = load_module()
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = module.main(['--min-guides', '1', *extra, str(root)])
    return code, out.getvalue() + err.getvalue()


def guide(rows=None, *, stated=63, grade='C') -> str:
    """A well-formed haproxy guide. Defaults are the real, correct page."""
    rows = CANONICAL_ROWS if rows is None else rows
    table = '\n'.join(
        f'| {t} | {v} | {s}/{m} | {d} |' for t, v, s, m, d in rows
    )
    return f'''---
title: "How to harden a HAProxy container: haproxy:3.1-alpine scores {stated}/100 by default"
description: "haproxy:3.1-alpine defaults score {stated}/100 (grade {grade}): full caps, writable rootfs."
---

# How to harden a HAProxy container

Graded on IronClaw's seven-dimension containment scale, the default configuration scores
**{stated} of 100, grade {grade} (partial)**. Higher is safer. A few runtime flags take the same
image to **89 of 100, grade B**.

## Where the default configuration leaks

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
{table}

## Before and after

```bash
# Before: {stated}/100, grade {grade}
docker run -d --name haproxy haproxy:3.1-alpine

# After: 89/100, grade B
docker run -d --name haproxy-hardened haproxy:3.1-alpine
```
'''


class Fixture:
    """A minimal repo: the real score.go, generator and results.json, one guide."""

    def __init__(self, stack: contextlib.ExitStack, text: str | None = None,
                 hub: str | None = None):
        self.root = pathlib.Path(stack.enter_context(tempfile.TemporaryDirectory()))
        for rel in ('internal/host/scan/score.go',
                    'examples/isolation-survey/gen_scorecards.py',
                    'examples/isolation-survey/results.json'):
            dst = self.root / rel
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(REPO_ROOT / rel, dst)
        blog = self.root / 'docs' / 'blog'
        blog.mkdir(parents=True)
        (blog / 'harden-haproxy-container-isolation.md').write_text(
            guide() if text is None else text, encoding='utf-8')
        if hub is not None:
            (blog / 'hardening-guides.md').write_text(hub, encoding='utf-8')

    def score_go(self) -> pathlib.Path:
        return self.root / 'internal/host/scan/score.go'


class CheckGuideScoresTest(unittest.TestCase):

    def setUp(self):
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)

    # ---- positive control: the checker must be silent on correct input ---- #

    def test_correct_guide_passes(self):
        code, out = run(Fixture(self.stack).root)
        self.assertEqual(code, 0, out)
        self.assertIn('violations: 0', out)

    def test_real_corpus_is_clean(self):
        """The repository's own 56 guides. A rule that fires here is wrong."""
        code, out = run(REPO_ROOT, '--min-guides', '56')
        self.assertEqual(code, 0, out)
        self.assertIn('guides parsed: 56', out)

    def test_canonical_set_matches(self):
        """Guard the guard: an empty canonical set makes every table conformant."""
        module = load_module()
        dims = module.parse_scorers((REPO_ROOT / module.SCORE_GO).read_text())
        self.assertEqual(len(dims), 7)
        self.assertEqual(sum(d.max for d in dims), 100)
        self.assertEqual([d.title for d in dims],
                         [r[0] for r in CANONICAL_ROWS])

    # ---- the trigger: PR #661's five defects, in one file ---- #

    def test_pr661_haproxy_rewrite(self):
        rows = [
            ('Non-root user (uid != 0)', '✅ PASS', 15, 15, 'runs as haproxy (uid != 0)'),
            ('Dropped capabilities', '⚠️ WARN', 4, 20, 'default capability set retained'),
            ('Seccomp profile', '✅ PASS', 15, 15, 'seccomp profile active'),
            ('Network isolation / egress', '⚠️ WARN', 4, 15, 'standard bridge network mode'),
            ('Read-only root filesystem', '⚠️ WARN', 5, 15, 'root filesystem is writable'),
            ('Privilege escalation', '⚠️ WARN', 5, 10, 'no explicitly enforced no-new-privileges'),
            ('No shared host namespaces', '✅ PASS', 10, 10, 'no host PID/IPC/network sharing'),
        ]
        self.assertEqual(sum(r[2] for r in rows), 58)
        self.assertEqual(sum(r[3] for r in rows), 100,
                         'the camouflage: the max column still totals 100')
        code, out = run(Fixture(self.stack, guide(rows)).root)
        self.assertEqual(code, 1, out)
        # 1. arithmetic, at every one of the four sites that state 63
        for site in ('frontmatter title', 'frontmatter description',
                     'body prose', 'before-run comment'):
            self.assertIn(f'{site} states 63/100 but the table sums to 58', out)
        # 2. the invented dimension
        self.assertIn("row 'Privilege escalation' is not a dimension the scorer emits", out)
        # 3. the deleted canonical dimension
        self.assertIn("canonical dimension 'No docker.sock exposure' is missing", out)
        # 4. the reweighted rootfs row
        self.assertIn("row 'Read-only root filesystem' is out of 15, the scorer weights it 10", out)
        # 5. the unreachable verdict/score pairs
        self.assertIn("row 'Read-only root filesystem' claims 5/15 WARN, "
                      "which gradeReadonly cannot emit", out)
        self.assertIn("row 'Dropped capabilities' claims 4/20 WARN, "
                      "which gradeCaps cannot emit", out)

    def test_max_column_only_check_would_be_green(self):
        """Proves the #661 fixture is not caught by the naive check it evaded."""
        module = load_module()
        dims = module.parse_scorers((REPO_ROOT / module.SCORE_GO).read_text())
        self.assertEqual(sum(d.max for d in dims), 100)

    # ---- each rule, isolated ---- #

    def test_score_column_off_by_one(self):
        rows = [list(r) for r in CANONICAL_ROWS]
        rows[2][2] = 14  # seccomp 15 -> 14, total 62 against four claims of 63
        code, out = run(Fixture(self.stack, guide([tuple(r) for r in rows])).root)
        self.assertEqual(code, 1, out)
        self.assertIn('the table sums to 62', out)

    def test_stated_score_disagrees_with_measured_run(self):
        """All four sites agree with each other and with the table, and are still wrong."""
        rows = [list(r) for r in CANONICAL_ROWS]
        rows[2][2] = 14
        text = guide([tuple(r) for r in rows], stated=62)
        code, out = run(Fixture(self.stack, text).root)
        self.assertEqual(code, 1, out)
        self.assertIn('table sums to 62, default-haproxy scored 63', out)

    def test_grade_letter_must_match_band(self):
        code, out = run(Fixture(self.stack, guide(stated=63, grade='B')).root)
        self.assertEqual(code, 1, out)
        self.assertIn('states 63/100 grade B; 63 is grade C', out)

    def test_reordered_dimension_is_flagged(self):
        rows = list(CANONICAL_ROWS)
        rows[1], rows[2] = rows[2], rows[1]
        code, out = run(Fixture(self.stack, guide(rows)).root)
        self.assertEqual(code, 1, out)
        self.assertIn('the canonical order puts', out)

    def test_verdict_disagreeing_with_measured_run(self):
        rows = [list(r) for r in CANONICAL_ROWS]
        rows[4][1] = '⚠️ WARN'   # rootfs FAIL -> WARN, score untouched
        code, out = run(Fixture(self.stack, guide([tuple(r) for r in rows])).root)
        self.assertEqual(code, 1, out)
        self.assertIn("row 'Read-only root filesystem' claims 0/10 WARN, "
                      "which gradeReadonly cannot emit", out)

    def test_missing_score_site_is_flagged(self):
        text = guide().replace('# Before: 63/100, grade C', '# Before hardening')
        code, out = run(Fixture(self.stack, text).root)
        self.assertEqual(code, 1, out)
        self.assertIn('no default score stated in the before-run comment', out)

    def test_hub_row_drift_is_flagged(self):
        """The IRO-696 surface: the hub restates the total and can go stale alone."""
        hub = ('| Image | Before | After | Ceiling |\n|---|---|---|---|\n'
               '| [HAProxy](harden-haproxy-container-isolation.md) | 55/100 C '
               '| **89/100 B** | load balancer |\n')
        code, out = run(Fixture(self.stack, hub=hub).root)
        self.assertEqual(code, 1, out)
        self.assertIn('hub: haproxy row states 55/100, its guide table sums to 63', out)

    # ---- degenerate inputs must not be green ---- #

    def test_zero_guides_is_not_green(self):
        fixture = Fixture(self.stack)
        (fixture.root / 'docs/blog/harden-haproxy-container-isolation.md').unlink()
        code, out = run(fixture.root)
        self.assertEqual(code, 2, out)
        self.assertIn('parsed 0 guides', out)

    def test_guide_count_drop_is_not_green(self):
        code, out = run(REPO_ROOT, '--min-guides', '57')
        self.assertEqual(code, 2, out)
        self.assertIn('expected at least 57', out)

    def test_missing_table_is_flagged(self):
        text = guide().split('| Dimension |')[0] + '\nNo table here.\n'
        code, out = run(Fixture(self.stack, text).root)
        self.assertEqual(code, 1, out)
        self.assertIn('no dimension table found', out)

    def test_unparseable_scorers_slice_exits_two(self):
        """A canonical set that cannot be read must fail loudly, not pass empty."""
        fixture = Fixture(self.stack)
        path = fixture.score_go()
        path.write_text(path.read_text().replace('var scorers = []scorer{',
                                                 'var renamedScorers = []scorer{'))
        code, out = run(fixture.root)
        self.assertEqual(code, 2, out)
        self.assertIn('could not find `var scorers', out)

    def test_reweighted_scorer_is_followed_not_hardcoded(self):
        """Change score.go and the *guide* becomes the violation. No second copy."""
        fixture = Fixture(self.stack)
        path = fixture.score_go()
        path.write_text(path.read_text()
                        .replace('"Seccomp profile", 15,', '"Seccomp profile", 14,')
                        .replace('const TotalWeight = 100', 'const TotalWeight = 99'))
        code, out = run(fixture.root)
        self.assertEqual(code, 1, out)
        self.assertIn("row 'Seccomp profile' is out of 15, the scorer weights it 14", out)

    def test_scorer_mirror_divergence_is_reported(self):
        """score.go vs the published methodology: a divergence is a product bug."""
        fixture = Fixture(self.stack)
        gen = fixture.root / 'examples/isolation-survey/gen_scorecards.py'
        gen.write_text(gen.read_text().replace(
            '"- **Dropped capabilities** (20 pts)', '"- **Dropped capabilities** (25 pts)'))
        code, out = run(fixture.root)
        self.assertEqual(code, 1, out)
        self.assertIn('canonical-mirror:', out)
        self.assertIn('weighs 25', out)

    def test_unmeasured_image_is_flagged(self):
        """A guide with no default-* scenario cannot be checked and must say so."""
        fixture = Fixture(self.stack)
        results = fixture.root / 'examples/isolation-survey/results.json'
        data = json.loads(results.read_text())
        data['scenarios'] = [s for s in data['scenarios']
                             if s.get('label') != 'default-haproxy'
                             and 'haproxy' not in s.get('image', '')]
        results.write_text(json.dumps(data))
        code, out = run(fixture.root)
        self.assertEqual(code, 1, out)
        self.assertIn('the table is unverifiable against measured data', out)

    # ---- rollout mode ---- #

    def test_report_mode_prints_but_does_not_fail(self):
        rows = [list(r) for r in CANONICAL_ROWS]
        rows[2][2] = 14
        code, out = run(Fixture(self.stack, guide([tuple(r) for r in rows])).root, '--report')
        self.assertEqual(code, 0, out)
        self.assertIn('the table sums to 62', out)
        self.assertIn('report mode: not failing the build', out)

    def test_report_mode_still_fails_on_zero_guides(self):
        """Report mode softens violations, never the non-vacuity assertion."""
        fixture = Fixture(self.stack)
        (fixture.root / 'docs/blog/harden-haproxy-container-isolation.md').unlink()
        code, out = run(fixture.root, '--report')
        self.assertEqual(code, 2, out)


if __name__ == '__main__':
    unittest.main()
