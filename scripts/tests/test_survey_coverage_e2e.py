#!/usr/bin/env python3
"""End-to-end test of survey.sh's skip recording + coverage guard (IRO-727).

The unit tests in test_coverage_guard.py exercise the guard's logic; this one
runs the actual `examples/isolation-survey/survey.sh` -- under `set -euo
pipefail`, through all of its skip paths -- against stub `docker` and `ironctl`
binaries. No daemon, no images, no network: the stubs are the only way to prove
that a pull/run/scan failure ends up in `results.json` rather than only in a run
log, which is the defect being fixed.

The tmpdir is a real git repo, because the baseline the guard compares against
is the last COMMITTED `results.json`, not the working-tree copy the run is about
to overwrite. `commit_results()` below is the test's stand-in for the weekly
refresh workflow committing a green run. That distinction is the whole of
test_a_rerun_does_not_launder_the_regression: with a working-tree baseline, run
N writes the degraded artifact and run N+1 reads it back as its own baseline, so
the second run of an unfixed sweep goes green and the guard is something you
learn to re-run through.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import collections
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent.parent
SURVEY_DIR = REPO_ROOT / 'examples' / 'isolation-survey'


def _load_guard():
    spec = importlib.util.spec_from_file_location(
        'coverage_guard', SURVEY_DIR / 'coverage_guard.py')
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


guard = _load_guard()

# A stub `docker` whose failure modes are keyed off the image/container name, so
# one manifest exercises every skip path. `image inspect` without --format is
# survey.sh's "is it cached?" probe and must miss, so the pull path runs.
DOCKER_STUB = r'''#!/usr/bin/env bash
set -uo pipefail
cmd="$1"; shift
case "$cmd" in
  info) exit 0 ;;
  image)
    sub="$1"; shift
    if [ "$sub" = "inspect" ]; then
      case " $* " in
        *--format*) echo "${1}@sha256:$(printf %064d 1)"; exit 0 ;;
        *) exit 1 ;;   # never cached: force the pull path
      esac
    fi
    exit 0 ;;
  pull)
    case " $* " in
      *bad-pull*) echo "Error response from daemon: manifest unknown" >&2; exit 1 ;;
    esac
    exit 0 ;;
  run)
    case " $* " in
      *bad-run*) echo 'docker: Error response from daemon: exec: "sleep": executable file not found in $PATH' >&2; exit 1 ;;
    esac
    echo "cid$$"; exit 0 ;;
  rm|rmi) exit 0 ;;
  *) exit 0 ;;
esac
'''

# A stub `ironctl` that emits a minimal but real-shaped scan report -- or, for
# `bad-json`, exits 0 with a body render.py cannot read.
IRONCTL_STUB = r'''#!/usr/bin/env bash
set -uo pipefail
[ "${1:-}" = "scan" ] || exit 1
shift
[ "${1:-}" = "--help" ] && exit 0
target="$1"
case "$target" in
  *bad-scan*) echo "scan: container is not running" >&2; exit 1 ;;
  *bad-json*) echo "<html>502 Bad Gateway</html>"; exit 0 ;;
esac
cat <<JSON
{"score": 42, "grade": "D", "generatedAt": "2026-01-01T00:00:00Z",
 "version": "test", "target": "$target",
 "dimensions": [{"key": "user", "title": "Non-root user", "verdict": "FAIL", "max": 20}]}
JSON
'''


def manifest(rows):
    return "# scenario | image | run flags\n" + "".join(
        f"{lab} | example/{lab}:1 |\n" for lab in rows)


class SurveyCoverageE2E(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.tmp.name)
        # survey.sh derives REPO_ROOT as ../.. from its own location.
        self.dir = self.root / 'examples' / 'isolation-survey'
        self.dir.mkdir(parents=True)
        for name in ('survey.sh', 'render.py', 'coverage_guard.py'):
            shutil.copy2(SURVEY_DIR / name, self.dir / name)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        for name, body in (('docker', DOCKER_STUB), ('ironctl', IRONCTL_STUB)):
            p = self.bin / name
            p.write_text(body)
            p.chmod(0o755)
        # The guard reads its baseline out of git, so the fixture has to be one.
        self.git('init', '-q')
        self.git('add', '-A')
        self.git('commit', '-qm', 'fixture')

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args):
        return subprocess.run(
            ['git', '-C', str(self.root),
             '-c', 'user.email=test@example.com', '-c', 'user.name=test',
             '-c', 'commit.gpgsign=false', *args],
            check=True, capture_output=True, text=True)

    def commit_results(self):
        """Stand in for the refresh workflow committing a green run. Only a run
        that passed the guard is ever committed, which is what makes HEAD a
        coverage level we actually held."""
        self.git('add', '-A')
        self.git('commit', '-qm', 'results')

    def write_manifest(self, text):
        (self.dir / 'images.txt').write_text(text)

    def run_survey(self, rows=None):
        if rows is not None:
            self.write_manifest(manifest(rows))
        env = dict(os.environ,
                   DOCKER=str(self.bin / 'docker'),
                   IRONCTL=str(self.bin / 'ironctl'),
                   MIRROR='0', PRUNE='0')
        return subprocess.run(['bash', str(self.dir / 'survey.sh')],
                              cwd=self.dir, env=env,
                              capture_output=True, text=True)

    def results(self):
        return json.loads((self.dir / 'results.json').read_text())

    def test_all_three_skip_stages_land_in_the_artifact(self):
        proc = self.run_survey(['ok-one', 'bad-pull', 'bad-run', 'bad-scan',
                                'ok-two'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        doc = self.results()

        self.assertEqual(doc['schemaVersion'], '1.1')
        self.assertEqual(doc['manifestRowCount'], 5)
        self.assertEqual(doc['scenarioCount'], 2)
        self.assertEqual(doc['skippedCount'], 3)
        self.assertEqual(
            {s['label']: s['stage'] for s in doc['skipped']},
            {'bad-pull': 'pull', 'bad-run': 'run', 'bad-scan': 'scan'})
        # scanned + skipped accounts for every manifest row -- the arithmetic a
        # reader could not previously do from the artifact alone, and which the
        # guard now checks rather than printing.
        self.assertEqual(doc['scenarioCount'] + doc['skippedCount'],
                         doc['manifestRowCount'])

        reasons = {s['label']: s['reason'] for s in doc['skipped']}
        self.assertIn('manifest unknown', reasons['bad-pull'])
        self.assertIn('sleep', reasons['bad-run'])
        self.assertIn('not running', reasons['bad-scan'])

        md = (self.dir / 'results.md').read_text()
        self.assertIn('Coverage: 2 of 5 manifest rows', md)
        self.assertIn('## Not scanned', md)
        for lab in ('bad-pull', 'bad-run', 'bad-scan'):
            self.assertIn(f'`{lab}`', md)

    def test_full_coverage_says_so(self):
        proc = self.run_survey(['ok-one', 'ok-two'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        doc = self.results()
        self.assertEqual(doc['skipped'], [])
        self.assertIn('every row was scanned',
                      (self.dir / 'results.md').read_text())

    def test_partial_coverage_never_claims_every_row_was_scanned(self):
        """The sentence has to come from the arithmetic. Reading it off an empty
        skip list printed "Coverage: 2 of 3 manifest rows -- every row was
        scanned" at exit 0, which is the fail-quiet this PR exists to remove."""
        proc = self.run_survey(['ok-one', 'bad-pull', 'ok-two'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        md = (self.dir / 'results.md').read_text()
        self.assertIn('Coverage: 2 of 3 manifest rows', md)
        self.assertNotIn('every row was scanned', md)

    def test_losing_a_row_that_scored_before_fails_the_run(self):
        first = self.run_survey(['ok-one', 'ok-two'])
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(self.results()['scenarioCount'], 2)
        self.commit_results()

        # Same manifest, but ok-two now fails to run. Under the old
        # `scanned > 0` floor this second run was green.
        self.write_manifest("ok-one | example/ok-one:1 |\n"
                            "ok-two | example/bad-run:1 |\n")
        second = self.run_survey()
        self.assertNotEqual(second.returncode, 0)
        self.assertIn('baseline: git HEAD:', second.stderr)
        self.assertIn('ok-two', second.stderr)
        self.assertIn('coverage regressed', second.stderr)

        # The artifact is written anyway, and explains itself.
        doc = self.results()
        self.assertEqual(doc['scenarioCount'], 1)
        self.assertEqual([s['label'] for s in doc['skipped']], ['ok-two'])
        self.assertEqual(doc['skipped'][0]['stage'], 'run')

    def test_a_rerun_does_not_launder_the_regression(self):
        """Run the identical broken sweep twice: both must fail.

        This is the finding the working-tree baseline had. Run N overwrote
        results.json with the degraded artifact and run N+1 snapshotted that
        file as its own baseline, so the second run saw no loss and exited 0
        with nothing fixed. The baseline now comes from `git show HEAD:`, and
        only a passing run is ever committed, so re-running cannot move it.
        """
        self.assertEqual(self.run_survey(['ok-one', 'ok-two']).returncode, 0)
        self.commit_results()

        broken = ("ok-one | example/ok-one:1 |\n"
                  "ok-two | example/bad-run:1 |\n")
        self.write_manifest(broken)

        for attempt in (1, 2, 3):
            proc = self.run_survey()
            self.assertNotEqual(
                proc.returncode, 0,
                f"run {attempt} of the identical unfixed sweep exited 0:\n"
                f"{proc.stderr}")
            self.assertIn('coverage regressed', proc.stderr)
            self.assertIn('ok-two', proc.stderr)
            # And the degraded artifact on disk is not what it compared against.
            self.assertEqual(self.results()['scenarioCount'], 1)

        # Fixing the row is what clears it -- the guard is not merely sticky.
        self.write_manifest(manifest(['ok-one', 'ok-two']))
        self.assertEqual(self.run_survey().returncode, 0)

    def test_a_run_where_every_row_fails_still_writes_the_artifact(self):
        """The case the README's durability claim is about. `scanned > 0` used
        to die before render.py ran, so a dead daemon or a mirror outage left
        the previous healthy results.json byte-identical on disk and cleanup()
        deleted every skip record on the way out."""
        self.assertEqual(self.run_survey(['ok-one', 'ok-two']).returncode, 0)
        self.commit_results()
        healthy = (self.dir / 'results.json').read_text()

        self.write_manifest("ok-one | example/bad-pull:1 |\n"
                            "ok-two | example/bad-pull:1 |\n")
        proc = self.run_survey()
        self.assertNotEqual(proc.returncode, 0)

        self.assertNotEqual((self.dir / 'results.json').read_text(), healthy,
                            "the all-rows-failed run left the previous "
                            "artifact in place instead of writing its own")
        doc = self.results()
        self.assertEqual(doc['scenarioCount'], 0)
        self.assertEqual(doc['skippedCount'], 2)
        self.assertEqual(doc['manifestRowCount'], 2)
        self.assertEqual({s['stage'] for s in doc['skipped']}, {'pull'})

        md = (self.dir / 'results.md').read_text()
        self.assertIn('No scenario produced a scorecard', md)
        for lab in ('ok-one', 'ok-two'):
            self.assertIn(f'`{lab}`', md)

    def test_an_unreadable_scan_report_is_a_skip_not_an_abort(self):
        """An exit-0 scan whose body is not JSON used to abort the whole run
        under `set -e`, before render.py, destroying every skip recorded so
        far."""
        proc = self.run_survey(['bad-pull', 'bad-json', 'ok-one'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        doc = self.results()
        self.assertEqual(doc['manifestRowCount'], 3)
        self.assertEqual(doc['scenarioCount'], 1)
        stages = {s['label']: s['stage'] for s in doc['skipped']}
        self.assertEqual(stages, {'bad-pull': 'pull', 'bad-json': 'scan'})
        # ...and the pull skip recorded BEFORE the unreadable report survived.
        self.assertIn('unreadable scan report',
                      [s['reason'] for s in doc['skipped']
                       if s['label'] == 'bad-json'][0])

    def test_manifest_parse_matches_the_sweep(self):
        """coverage_guard.manifest_labels claims to mirror survey.sh's parse, so
        prove it against survey.sh rather than against a literal in the test.

        Every row here is a case the two used to disagree on: an indented `#`
        (a comment to the guard's old `.strip()`, a row to the sweep), a
        repeated label (deduplicated by the guard, walked twice by the sweep),
        collapsed inner whitespace, and a final line with no trailing newline.
        """
        self.write_manifest(
            "# column-zero comment | ignored |\n"
            "\n"
            "ok-one | example/ok-one:1 |\n"
            "  # indented | example/ok-two:1 |\n"
            "ok-one | example/ok-one:1 |\n"
            "   spaced   label   |  example/bad-pull:1  |\n"
            "last-row | example/ok-three:1 |")  # no trailing newline

        proc = self.run_survey()
        self.assertEqual(proc.returncode, 0, proc.stderr)
        doc = self.results()

        parsed = guard.manifest_labels(self.dir / 'images.txt')
        # survey.sh's own count of the rows it walked.
        self.assertEqual(doc['manifestRowCount'], len(parsed), parsed)
        # ...and the same labels, with the same multiplicity.
        walked = [s['label'] for s in doc['scenarios']] + \
                 [s['label'] for s in doc['skipped']]
        self.assertEqual(collections.Counter(walked),
                         collections.Counter(parsed))
        self.assertIn('# indented', parsed)
        self.assertEqual(parsed.count('ok-one'), 2)
        self.assertIn('spaced label', parsed)
        self.assertIn('last-row', parsed)

    def test_retiring_a_dead_row_from_the_manifest_is_allowed(self):
        first = self.run_survey(['ok-one', 'ok-two'])
        self.assertEqual(first.returncode, 0, first.stderr)
        self.commit_results()
        second = self.run_survey(['ok-one'])
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.results()['scenarioCount'], 1)

    def test_a_never_scored_row_failing_is_tolerated(self):
        first = self.run_survey(['ok-one'])
        self.assertEqual(first.returncode, 0, first.stderr)
        self.commit_results()
        # A new row is added and its image is unavailable: skipped, recorded,
        # still green. One registry hiccup must not wedge the weekly refresh.
        second = self.run_survey(['ok-one', 'bad-pull'])
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual([s['label'] for s in self.results()['skipped']],
                         ['bad-pull'])

    def test_no_committed_baseline_says_the_check_was_skipped(self):
        """A first run has nothing to compare against. It must say so rather
        than quietly falling back to whatever results.json is lying on disk --
        that fallback is what made a re-run launder a regression."""
        proc = self.run_survey(['ok-one'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('baseline: none committed', proc.stderr)
        self.assertIn('UNCHECKED', proc.stderr)


class RenderCoverageSentence(unittest.TestCase):
    """render.py's coverage line, driven directly.

    survey.sh cannot currently produce a row that is neither scored nor
    recorded as skipped, so the self-contradicting sentence
    ("Coverage: 2 of 3 manifest rows -- every row was scanned") is only
    reachable by handing render.py the numbers. That is exactly why the
    sentence must be derived from the comparison instead of from `if skips:`:
    the day survey.sh grows a fourth drop path, the artifact should say what it
    has rather than what the skip list happens to be.
    """

    def render(self, labels, manifest_rows, skips=()):
        records = [{"label": lab, "image": f"example/{lab}:1",
                    "runFlags": "", "resolvedDigest": "",
                    "report": {"score": 42, "grade": "D",
                               "generatedAt": "2026-01-01T00:00:00Z",
                               "version": "test", "dimensions": []}}
                   for lab in labels]
        with tempfile.TemporaryDirectory() as d:
            d = pathlib.Path(d)
            argv = [str(SURVEY_DIR / 'render.py'),
                    str(d / 'results.json'), str(d / 'results.md'),
                    '--manifest-rows', str(manifest_rows)]
            if skips:
                (d / 'skips.json').write_text(json.dumps(
                    [{"label": lab, "image": f"example/{lab}:1",
                      "stage": stage, "reason": reason}
                     for lab, stage, reason in skips]))
                argv += ['--skips', str(d / 'skips.json')]
            proc = subprocess.run(['python3', *argv], input=json.dumps(records),
                                  capture_output=True, text=True)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            return (d / 'results.md').read_text()

    def test_an_unaccounted_row_is_never_called_full_coverage(self):
        md = self.render(['a', 'b'], manifest_rows=3)
        self.assertIn('Coverage: 2 of 3 manifest rows', md)
        self.assertNotIn('every row was scanned', md)
        self.assertIn('unaccounted for', md)

    def test_full_coverage_is_stated_only_when_the_counts_agree(self):
        md = self.render(['a', 'b'], manifest_rows=2)
        self.assertIn('every row was scanned', md)
        self.assertNotIn('unaccounted for', md)

    def test_skips_that_account_for_the_gap_are_not_reported_as_unaccounted(self):
        md = self.render(['a', 'b'], manifest_rows=3,
                         skips=[('c', 'pull', 'manifest unknown')])
        self.assertIn('Coverage: 2 of 3 manifest rows', md)
        self.assertNotIn('every row was scanned', md)
        self.assertNotIn('unaccounted for', md)


if __name__ == '__main__':
    unittest.main()
