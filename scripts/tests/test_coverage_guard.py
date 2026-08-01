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


def write_results(path, labels, skipped=()):
    doc = {
        "report": "ironclaw-isolation-survey",
        "schemaVersion": "1.1",
        "scenarioCount": len(labels),
        "scenarios": [{"label": lab, "score": 50} for lab in labels],
        "skippedCount": len(skipped),
        "skipped": [{"label": lab, "image": lab, "stage": stage,
                     "reason": reason} for lab, stage, reason in skipped],
    }
    path.write_text(json.dumps(doc))


def write_manifest(path, labels):
    lines = ["# scenario | image | run flags", ""]
    lines += [f"{lab} | example/{lab}:1 |" for lab in labels]
    path.write_text("\n".join(lines) + "\n")


class ManifestParsing(unittest.TestCase):
    def test_matches_survey_sh_parse(self):
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d) / 'images.txt'
            p.write_text(
                "# a comment\n"
                "\n"
                "default-nginx | nginx:1.27 |\n"
                "hardened-nginx | nginx:1.27 | --user 101 --cap-drop=ALL\n"
                "   spaced   |  busybox:1  |\n"
                "default-nginx | nginx:1.27 |\n"  # duplicate label
            )
            self.assertEqual(guard.manifest_labels(p),
                             ['default-nginx', 'hardened-nginx', 'spaced'])


class RegressionDetection(unittest.TestCase):
    """The core property: a row that scored before and stopped is a failure."""

    def run_guard(self, manifest, scanned, baseline, skipped=(), extra=None):
        with tempfile.TemporaryDirectory() as d:
            d = pathlib.Path(d)
            write_manifest(d / 'images.txt', manifest)
            write_results(d / 'results.json', scanned, skipped)
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

    def test_failure_names_the_stage_when_recorded(self):
        rc, err = self.run_guard(
            manifest=['a', 'b'], scanned=['a'], baseline=['a', 'b'],
            skipped=[('b', 'run', 'exec: "sleep": not found')])
        self.assertEqual(rc, 1)
        self.assertIn('failed at run', err)
        self.assertIn('sleep', err)

    def test_failure_without_a_skip_record_is_still_reported(self):
        """A schema-1.0 results.json has nowhere to record skips; the guard must
        still fail, just without an explanation."""
        rc, err = self.run_guard(
            manifest=['a', 'b'], scanned=['a'], baseline=['a', 'b'])
        self.assertEqual(rc, 1)
        self.assertIn('never reached the sweep', err)

    def test_the_real_39_row_shape_fails(self):
        """The exact production shape: 295 rows, 256 scanned, all 39 of the
        missing ones scored in the baseline. This is the run that used to be
        green."""
        manifest = [f'row-{i}' for i in range(295)]
        scanned = manifest[:256]
        rc, err = self.run_guard(manifest=manifest, scanned=scanned,
                                 baseline=manifest)
        self.assertEqual(rc, 1)
        self.assertIn('39 scenario(s)', err)

    def test_clean_run_passes(self):
        rc, _ = self.run_guard(manifest=['a', 'b'], scanned=['a', 'b'],
                               baseline=['a', 'b'])
        self.assertEqual(rc, 0)

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

    def test_no_baseline_says_so_instead_of_pretending(self):
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a'],
                                 baseline=None)
        self.assertEqual(rc, 0)
        self.assertIn('UNCHECKED', err)

    def test_no_baseline_still_enforces_a_floor(self):
        rc, err = self.run_guard(manifest=['a', 'b'], scanned=['a'],
                                 baseline=None, extra=['--min-scanned', '2'])
        self.assertEqual(rc, 1)
        self.assertIn('floor is 2', err)

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
        self.assertEqual(len(regressed), len(manifest) - len(scanned))
        self.assertTrue(all(lab.startswith('default-') for lab in regressed))


if __name__ == '__main__':
    unittest.main()
