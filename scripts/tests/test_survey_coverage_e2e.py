#!/usr/bin/env python3
"""End-to-end test of survey.sh's skip recording + coverage guard (IRO-727).

The unit tests in test_coverage_guard.py exercise the guard's logic; this one
runs the actual `examples/isolation-survey/survey.sh` -- under `set -euo
pipefail`, through all three of its skip paths -- against stub `docker` and
`ironctl` binaries. No daemon, no images, no network: the stubs are the only
way to prove that a pull/run/scan failure ends up in `results.json` rather than
only in a run log, which is the defect being fixed.

Two runs, in order, because the guard is about the delta between them:

 1. every row succeeds -> results.json is the baseline, guard passes.
 2. a row that scored in (1) now fails -> survey.sh exits NON-ZERO, and
    results.json still exists and names the row and its stage. Writing the
    artifact even on failure is deliberate: the evidence has to outlive the run.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent.parent
SURVEY_DIR = REPO_ROOT / 'examples' / 'isolation-survey'

# A stub `docker` whose failure modes are keyed off the image/container name, so
# one manifest exercises all three skip paths. `image inspect` without --format
# is survey.sh's "is it cached?" probe and must miss, so the pull path runs.
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

# A stub `ironctl` that emits a minimal but real-shaped scan report.
IRONCTL_STUB = r'''#!/usr/bin/env bash
set -uo pipefail
[ "${1:-}" = "scan" ] || exit 1
shift
[ "${1:-}" = "--help" ] && exit 0
target="$1"
case "$target" in
  *bad-scan*) echo "scan: container is not running" >&2; exit 1 ;;
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
        root = pathlib.Path(self.tmp.name)
        # survey.sh derives REPO_ROOT as ../.. from its own location.
        self.dir = root / 'examples' / 'isolation-survey'
        self.dir.mkdir(parents=True)
        for name in ('survey.sh', 'render.py', 'coverage_guard.py'):
            shutil.copy2(SURVEY_DIR / name, self.dir / name)
        self.bin = root / 'bin'
        self.bin.mkdir()
        for name, body in (('docker', DOCKER_STUB), ('ironctl', IRONCTL_STUB)):
            p = self.bin / name
            p.write_text(body)
            p.chmod(0o755)

    def tearDown(self):
        self.tmp.cleanup()

    def run_survey(self, rows):
        (self.dir / 'images.txt').write_text(manifest(rows))
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
        # reader could not previously do from the artifact alone.
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

    def test_losing_a_row_that_scored_before_fails_the_run(self):
        first = self.run_survey(['ok-one', 'ok-two'])
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(self.results()['scenarioCount'], 2)

        # Same manifest, but ok-two now fails to run. Under the old
        # `scanned > 0` floor this second run was green.
        (self.dir / 'images.txt').write_text(
            "ok-one | example/ok-one:1 |\n"
            "ok-two | example/bad-run:1 |\n")
        env = dict(os.environ,
                   DOCKER=str(self.bin / 'docker'),
                   IRONCTL=str(self.bin / 'ironctl'),
                   MIRROR='0', PRUNE='0')
        second = subprocess.run(['bash', str(self.dir / 'survey.sh')],
                                cwd=self.dir, env=env,
                                capture_output=True, text=True)
        self.assertNotEqual(second.returncode, 0)
        self.assertIn('ok-two', second.stderr)
        self.assertIn('coverage regressed', second.stderr)

        # The artifact is written anyway, and explains itself.
        doc = self.results()
        self.assertEqual(doc['scenarioCount'], 1)
        self.assertEqual([s['label'] for s in doc['skipped']], ['ok-two'])
        self.assertEqual(doc['skipped'][0]['stage'], 'run')

    def test_retiring_a_dead_row_from_the_manifest_is_allowed(self):
        first = self.run_survey(['ok-one', 'ok-two'])
        self.assertEqual(first.returncode, 0, first.stderr)
        second = self.run_survey(['ok-one'])
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.results()['scenarioCount'], 1)

    def test_a_never_scored_row_failing_is_tolerated(self):
        first = self.run_survey(['ok-one'])
        self.assertEqual(first.returncode, 0, first.stderr)
        # A new row is added and its image is unavailable: skipped, recorded,
        # still green. One registry hiccup must not wedge the weekly refresh.
        second = self.run_survey(['ok-one', 'bad-pull'])
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual([s['label'] for s in self.results()['skipped']],
                         ['bad-pull'])


if __name__ == '__main__':
    unittest.main()
