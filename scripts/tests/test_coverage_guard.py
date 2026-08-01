#!/usr/bin/env python3
"""Tests for examples/isolation-survey/coverage_guard.py (IRO-727).

The defect this guard replaces was fail-quiet: 39 of 295 manifest rows produced
no output every single week and `scanned > 0` still called the run green. A test
that only asserts "clean input passes" would have passed against the broken code
too, so every case below is a negative control -- each builds a run that DID
lose coverage and asserts the guard refuses to call it green -- plus the two
carve-outs that would make the guard unusable if it got them wrong:

* test_row_deleted_from_manifest_is_not_a_regression -- retiring a permanently
  dead row is the intended fix, so a label that leaves images.txt must not fail
  forever after.
* test_transient_failure_absent_from_baseline_does_not_fail -- a row that has
  never scored (registry weather on a brand-new row) is not a regression, which
  is what keeps the weekly refresh from going red on a Docker Hub 500.

The one thing NOT tested here is whether `manifest_labels` really agrees with
survey.sh's parse. That cannot be settled against a literal in this file, so it
is settled by driving the real survey.sh over a hostile manifest and diffing the
two row sets: test_survey_coverage_e2e.py::test_manifest_parse_matches_the_sweep.
What lives here is the corrected semantics that test pins down.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import pathlib
import tempfile
import unittest

SCRIPT = (pathlib.Path(__file__).resolve().parent.parent.parent
          / 'examples' / 'isolation-survey' / 'coverage_guard.py')


def _load():
    spec = importlib.util.spec_from_file_location('coverage_guard', SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


guard = _load()


def results_doc(labels, skipped=(), manifest_rows=None, schema='1.1'):
    """A results.json shaped like render.py writes one.

    `manifest_rows=None` means "self-consistent": scored + skipped, so a test
    that is about the regression logic does not accidentally also trip the
    accounting check. Pass a number to build the inconsistent artifact on
    purpose. `schema='1.0'` drops the coverage block entirely, which is what the
    committed results.json looks like today.
    """
    doc = {
        "report": "ironclaw-isolation-survey",
        "schemaVersion": schema,
        "scenarios": [{"label": lab, "score": 50} for lab in labels],
        "skipped": [{"label": lab, "image": lab, "stage": stage,
                     "reason": reason} for lab, stage, reason in skipped],
    }
    if schema != '1.0':
        if manifest_rows is None:
            manifest_rows = len(labels) + len(skipped)
        doc["manifestRowCount"] = manifest_rows
        doc["scenarioCount"] = len(labels)
        doc["skippedCount"] = len(skipped)
    return doc


def write_results(path, labels, skipped=(), manifest_rows=None, schema='1.1'):
    path.write_text(json.dumps(
        results_doc(labels, skipped, manifest_rows, schema)))


def write_manifest(path, labels):
    lines = ["# scenario | image | run flags", ""]
    lines += [f"{lab} | example/{lab}:1 |" for lab in labels]
    path.write_text("\n".join(lines) + "\n")


class ManifestParsing(unittest.TestCase):
    """The corrected parse. Every case here is a row survey.sh walks and the
    guard used to disagree about -- a disagreement means the guard computes
    `expected` over a different row set than the sweep swept."""

    def labels(self, text):
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d) / 'images.txt'
            p.write_text(text)
            return guard.manifest_labels(p)

    def test_column_zero_comments_and_blanks_are_skipped(self):
        self.assertEqual(
            self.labels("# a comment\n\ndefault-nginx | nginx:1.27 |\n"),
            ['default-nginx'])

    def test_an_indented_comment_is_a_row_not_a_comment(self):
        """survey.sh matches `#` at column 0 only (`case "$line" in '#'*`), so
        an indented `#` is a scenario row to the sweep. The guard used to
        `.strip()` first and drop it, quietly shrinking its denominator."""
        self.assertEqual(self.labels("  # indented | busybox:1 |\n"),
                         ['# indented'])

    def test_duplicate_labels_are_kept(self):
        """The sweep walks a repeated row twice and counts it twice; the guard
        used to deduplicate, so the two disagreed on manifestRowCount."""
        self.assertEqual(
            self.labels("a | x:1 |\nb | y:1 |\na | x:1 |\n"), ['a', 'b', 'a'])

    def test_whitespace_is_collapsed_like_xargs(self):
        self.assertEqual(self.labels("   two   words   |  busybox:1  |\n"),
                         ['two words'])

    def test_trailing_cr_is_stripped(self):
        self.assertEqual(self.labels("crlf-row | busybox:1 |\r\n"),
                         ['crlf-row'])

    def test_a_row_with_no_label_is_dropped(self):
        self.assertEqual(self.labels("   \n | busybox:1 |\nreal | b:1 |\n"),
                         ['real'])

    def test_a_last_line_without_a_newline_still_counts(self):
        self.assertEqual(self.labels("a | x:1 |\nb | y:1 |"), ['a', 'b'])


class Accounting(unittest.TestCase):
    """`scenarioCount + skippedCount == manifestRowCount == rows parsed here`.

    The PR that added the coverage block advertised this invariant and enforced
    it nowhere, so results.md could print "Coverage: 2 of 3 manifest rows --
    every row was scanned" and exit 0.
    """

    def check(self, doc, manifest):
        return guard.check_accounting(doc, manifest)

    def test_a_consistent_artifact_has_no_problems(self):
        doc = results_doc(['a', 'b'], skipped=[('c', 'pull', 'nope')])
        self.assertEqual(self.check(doc, ['a', 'b', 'c']), [])

    def test_an_unaccounted_row_is_a_failure(self):
        """256 scored + 0 recorded skips against 295 manifest rows: the exact
        production shape, where 39 rows vanished with no record at all."""
        doc = results_doc([f'r{i}' for i in range(256)], manifest_rows=295)
        problems = self.check(doc, [f'r{i}' for i in range(295)])
        self.assertTrue(any('39 manifest row(s) are unaccounted for' in p
                            for p in problems), problems)

    def test_a_missing_count_is_not_read_as_zero(self):
        """A schema-1.0 artifact records none of the three. Defaulting them to
        0 would make 0 + 0 == 0 'hold' -- the same fail-quiet shape as the bug
        being fixed -- so it must report the counts as unverified instead."""
        doc = results_doc(['a'], schema='1.0')
        problems = self.check(doc, ['a', 'b'])
        self.assertEqual(len(problems), 1)
        self.assertIn('does not record', problems[0])
        for field in ('manifestRowCount', 'scenarioCount', 'skippedCount'):
            self.assertIn(field, problems[0])

    def test_a_counter_that_lies_about_its_own_array_fails(self):
        doc = results_doc(['a', 'b'])
        doc['scenarioCount'] = 5
        problems = self.check(doc, ['a', 'b'])
        self.assertTrue(any('scenarioCount is 5' in p for p in problems),
                        problems)

    def test_the_sweep_and_the_guard_disagreeing_on_the_manifest_fails(self):
        """The live detector for a parse drift: survey.sh counts the rows it
        walked into manifestRowCount, this guard parses images.txt itself, and
        the two numbers have to be the same one."""
        doc = results_doc(['a', 'b'], manifest_rows=2)
        problems = self.check(doc, ['a', 'b', '# indented'])
        self.assertTrue(any('the sweep and the guard disagree' in p
                            for p in problems), problems)

    def test_more_output_than_the_manifest_asked_for_fails(self):
        doc = results_doc(['a', 'b', 'c'], manifest_rows=2)
        problems = self.check(doc, ['a', 'b'])
        self.assertTrue(any('exceeds manifestRowCount' in p for p in problems),
                        problems)


class RegressionDetection(unittest.TestCase):
    """The core property: a row that scored before and stopped is a failure."""

    def run_guard(self, manifest, scanned, baseline, skipped=(), extra=None,
                  manifest_rows=None, schema='1.1'):
        with tempfile.TemporaryDirectory() as d:
            d = pathlib.Path(d)
            write_manifest(d / 'images.txt', manifest)
            if manifest_rows is None and schema != '1.0':
                manifest_rows = len(manifest)
            write_results(d / 'results.json', scanned, skipped, manifest_rows,
                          schema)
            argv = ['--manifest', str(d / 'images.txt'),
                    '--results', str(d / 'results.json')]
            if baseline is not None:
                write_results(d / 'baseline.json', baseline)
                argv += ['--baseline', str(d / 'baseline.json')]
            argv += extra or []
            err = io.StringIO()
            with contextlib.redirect_stderr(err):
                rc = guard.main(argv)
            return rc, err.getvalue()

    def test_dropped_row_fails(self):
        rc, err = self.run_guard(
            manifest=['a', 'b', 'c'], scanned=['a', 'b'], baseline=['a', 'b', 'c'],
            skipped=[('c', 'pull', 'manifest unknown')])
        self.assertEqual(rc, 1)
        self.assertIn('c: failed at pull: manifest unknown', err)
        # ...and for the regression, not as a side effect of bad arithmetic.
        self.assertNotIn('unaccounted', err)

    def test_failure_names_the_stage_when_recorded(self):
        rc, err = self.run_guard(
            manifest=['a', 'b'], scanned=['a'], baseline=['a', 'b'],
            skipped=[('b', 'run', 'exec: "sleep": not found')])
        self.assertEqual(rc, 1)
        self.assertIn('failed at run', err)
        self.assertIn('sleep', err)

    def test_failure_without_a_skip_record_is_still_reported(self):
        """A schema-1.0 results.json has nowhere to record skips; the guard must
        still fail on the regression, and must separately say that the coverage
        invariant could not be checked rather than assume it held."""
        rc, err = self.run_guard(
            manifest=['a', 'b'], scanned=['a'], baseline=['a', 'b'],
            schema='1.0')
        self.assertEqual(rc, 1)
        self.assertIn('never reached the sweep', err)
        self.assertIn('does not record', err)

    def test_the_real_39_row_shape_fails(self):
        """The exact production shape: 295 rows, 256 scanned, all 39 of the
        missing ones scored in the baseline, schema 1.0 so nothing recorded the
        loss. This is the run that used to be green."""
        manifest = [f'row-{i}' for i in range(295)]
        scanned = manifest[:256]
        rc, err = self.run_guard(manifest=manifest, scanned=scanned,
                                 baseline=manifest, schema='1.0')
        self.assertEqual(rc, 1)
        self.assertIn('39 scenario(s)', err)

    def test_clean_run_passes(self):
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a', 'b'],
                                 baseline=['a', 'b'])
        self.assertEqual(rc, 0)
        # No implicit floor once a baseline exists: `regressed == []` already
        # implies `len(scanned) >= len(expected)`, so reporting one would be
        # theatre (and the branch enforcing it was unreachable).
        self.assertIn('no floor, regression-relative', err)

    def test_new_row_scoring_for_the_first_time_passes(self):
        rc, _ = self.run_guard(manifest=['a', 'b', 'c'], scanned=['a', 'b', 'c'],
                               baseline=['a', 'b'])
        self.assertEqual(rc, 0)

    def test_transient_failure_absent_from_baseline_does_not_fail(self):
        """A row that has never produced output is not a regression -- this is
        what stops one Docker Hub 500 from wedging the weekly refresh."""
        rc, _ = self.run_guard(manifest=['a', 'b', 'c'], scanned=['a', 'b'],
                               baseline=['a', 'b'],
                               skipped=[('c', 'pull', 'toomanyrequests')])
        self.assertEqual(rc, 0)

    def test_row_deleted_from_manifest_is_not_a_regression(self):
        """Deleting a dead row is the sanctioned fix, so the baseline must be
        intersected with the current manifest, not used raw."""
        rc, _ = self.run_guard(manifest=['a', 'b'], scanned=['a', 'b'],
                               baseline=['a', 'b', 'retired'])
        self.assertEqual(rc, 0)

    def test_a_duplicated_row_is_reported_once(self):
        """The manifest may repeat a label; the regression list should name it
        once rather than once per row."""
        rc, err = self.run_guard(
            manifest=['a', 'dup', 'dup'], scanned=['a'], baseline=['a', 'dup'],
            skipped=[('dup', 'pull', 'gone'), ('dup', 'pull', 'gone')])
        self.assertEqual(rc, 1)
        self.assertIn('1 scenario(s)', err)

    def test_no_baseline_says_so_instead_of_pretending(self):
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a'],
                                 baseline=None,
                                 skipped=[('b', 'pull', 'nope')])
        self.assertEqual(rc, 0)
        self.assertIn('UNCHECKED', err)

    def test_no_baseline_still_enforces_a_floor(self):
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a'],
                                 baseline=None,
                                 skipped=[('b', 'pull', 'nope')],
                                 extra=['--min-scanned', '2'])
        self.assertEqual(rc, 1)
        self.assertIn('floor is 2', err)

    def test_min_scanned_still_bites_with_a_baseline(self):
        """`--min-scanned` is the only floor left, and it has to mean something
        independent of the regression check or it is decoration. Here nothing
        regressed -- `b` never scored before -- and the explicit floor is what
        fails the run."""
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a'],
                                 baseline=['a'],
                                 skipped=[('b', 'pull', 'nope')],
                                 extra=['--min-scanned', '2'])
        self.assertEqual(rc, 1)
        self.assertIn('floor is 2', err)
        self.assertNotIn('scored in the previous results.json', err)

    def test_unreadable_input_is_exit_2_not_a_pass(self):
        with tempfile.TemporaryDirectory() as d:
            d = pathlib.Path(d)
            write_manifest(d / 'images.txt', ['a'])
            err = io.StringIO()
            with contextlib.redirect_stderr(err):
                rc = guard.main(['--manifest', str(d / 'images.txt'),
                                 '--results', str(d / 'nope.json')])
            self.assertEqual(rc, 2)


class RealRepo(unittest.TestCase):
    """Non-vacuity anchor against the committed tree, in both directions."""

    ROOT = SCRIPT.parent
    MANIFEST = ROOT / 'images.txt'
    RESULTS = ROOT / 'results.json'

    def test_committed_results_is_self_consistent(self):
        scanned = guard.scenario_labels(self.RESULTS)
        manifest = guard.manifest_labels(self.MANIFEST)
        self.assertGreater(len(manifest), 0)
        # Every scored label maps back to a manifest row: the survey never
        # invents scenarios, which is what makes the missing-rows arithmetic
        # (manifest - scanned) trustworthy.
        self.assertEqual(sorted(scanned - set(manifest)), [])

    def test_committed_results_would_fail_the_guard_against_a_full_baseline(self):
        """The live proof that the guard bites: pretend a previous run had
        covered the whole manifest, and the committed results.json -- the exact
        artifact that shipped green -- must be rejected."""
        scanned = guard.scenario_labels(self.RESULTS)
        manifest = guard.manifest_labels(self.MANIFEST)
        ok, floor, regressed, _ = guard.evaluate(manifest, scanned,
                                                 baseline=set(manifest))
        self.assertFalse(ok)
        # Exactly the rows the manifest asks for and the artifact never scored.
        # Deliberately NOT asserting anything about what those labels are named:
        # the weekly refresh regenerates results.json, so pinning the shape of
        # the currently-missing rows would turn an unrelated registry outage
        # into a red unit test.
        self.assertEqual(sorted(regressed), sorted(set(manifest) - scanned))
        self.assertEqual(len(regressed), len(set(manifest)) - len(scanned))


if __name__ == '__main__':
    unittest.main()
