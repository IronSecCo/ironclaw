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
import sys
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
# `target` is deliberately a constant rather than "$target". A scenario label is
# arbitrary text -- the parse tests drive labels containing a double quote and a
# bare CR through here -- and interpolating one into JSON unescaped emits a
# malformed report, which the sweep then correctly records as an unreadable-scan
# SKIP. That would be the stub failing the row, not the code under test.
cat <<JSON
{"score": 42, "grade": "D", "generatedAt": "2026-01-01T00:00:00Z",
 "version": "test", "target": "stub",
 "dimensions": [{"key": "user", "title": "Non-root user", "verdict": "FAIL", "max": 20}]}
JSON
'''


# A python3 that fails ONLY the record_skip write, the way a full temp
# filesystem does. record_skip is the one write PRUNE=1 makes likely to hit
# ENOSPC -- PRUNE exists because the runner's disk is nearly full, and /tmp
# shares that filesystem -- and it used to return 1 under `set -euo pipefail`,
# aborting the sweep before render and taking every skip collected so far with
# it via cleanup(). Everything else (append_record, the summary parse,
# render.py, coverage_guard.py) is delegated to the real interpreter.
PY_ENOSPC_SHIM = r'''#!/usr/bin/env bash
REAL="__REAL_PYTHON__"
if [ "${1:-}" = "-" ]; then
  script="$(cat)"
  case "$script" in
    *skipfile*)
      echo 'Traceback (most recent call last):' >&2
      echo 'OSError: [Errno 28] No space left on device' >&2
      exit 1 ;;
  esac
  printf '%s' "$script" | "$REAL" "$@"
  exit $?
fi
exec "$REAL" "$@"
'''

# Labels survey.sh and coverage_guard.py used to parse DIFFERENTLY, at an
# identical row count. Each is (name, label). The mechanism differs per row --
# xargs quote processing, xargs backslash processing, xargs defaulting to
# /bin/echo and eating a leading option, Python's Unicode-aware str.split(),
# Python's universal-newline file iteration -- but the consequence is one
# shared, silent failure: the sweep records the row under one spelling, the
# guard looks for the other, so the row never matches the baseline and a real
# permanent regression on it exits 0 forever.
DIVERGENT_LABELS = [
    ('plain', 'plain'),                       # control: must be LOUD either way
    ('double quotes', 'say "hi"'),
    ('backslash', 'a\\b'),
    ('nbsp', 'a\u00a0b'),  # explicit: an invisible literal is unreviewable
    ('leading dash-n', '-n foo'),
    ('bare cr', 'a\rb'),
]


def manifest(rows):
    return "# scenario | image | run flags\n" + "".join(
        f"{lab} | example/{lab}:1 |\n" for lab in rows)


class SurveyFixture(unittest.TestCase):
    """A throwaway repo with stub docker/ironctl, and the helpers to drive
    survey.sh inside it. Holds no tests of its own so the classes below do not
    each re-run every other class's cases."""

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
        # Bytes, not text: a manifest row can legitimately contain a bare CR,
        # and writing it through text mode would let the platform newline
        # translation rewrite the very byte under test.
        (self.dir / 'images.txt').write_bytes(text.encode())

    def run_survey(self, rows=None, extra_env=None):
        if rows is not None:
            self.write_manifest(manifest(rows))
        env = dict(os.environ,
                   DOCKER=str(self.bin / 'docker'),
                   IRONCTL=str(self.bin / 'ironctl'),
                   MIRROR='0', PRUNE='0')
        env.update(extra_env or {})
        return subprocess.run(['bash', str(self.dir / 'survey.sh')],
                              cwd=self.dir, env=env,
                              capture_output=True, text=True)

    def results(self):
        return json.loads((self.dir / 'results.json').read_text())


class SurveyCoverageE2E(SurveyFixture):
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

    def test_a_parse_divergence_never_produces_a_silent_regression(self):
        """The control table for the residual parse gaps.

        For each label the two parsers used to spell differently: score it,
        commit it as the baseline, then break that exact row and demand the
        guard is LOUD. Under the old code the sweep recorded one spelling and
        the guard looked for the other, so the row silently left `expected` and
        a permanent regression on it printed 'coverage: 1 scanned / 2 manifest
        rows' and exited 0 -- IRO-727 reproduced inside the guard written to
        prevent it. `plain` is the positive control: it was loud before this
        change too, so a harness that cannot fail here proves nothing.
        """
        for i, (name, label) in enumerate(DIVERGENT_LABELS):
            if i:
                # A fresh repo per row; the outer tearDown cleans the last one.
                self.tearDown()
                self.setUp()
            with self.subTest(divergence=name, label=label):
                self.write_manifest(f"{label} | example/ok-one:1 |\n"
                                    "keeper | example/ok-two:1 |\n")
                first = self.run_survey()
                self.assertEqual(first.returncode, 0, first.stderr)
                # The sweep recorded the label verbatim...
                doc = self.results()
                self.assertIn(label, [s['label'] for s in doc['scenarios']],
                              "the sweep rewrote the label while recording it")
                # ...and the guard parses the identical string out of the same
                # manifest. This is the equality everything else rests on.
                self.assertEqual(
                    collections.Counter(
                        guard.manifest_labels(self.dir / 'images.txt')),
                    collections.Counter(
                        [s['label'] for s in doc['scenarios']]
                        + [s['label'] for s in doc['skipped']]))
                self.commit_results()

                # Now that row rots for good. This must be loud.
                self.write_manifest(f"{label} | example/bad-run:1 |\n"
                                    "keeper | example/ok-two:1 |\n")
                second = self.run_survey()
                self.assertNotEqual(
                    second.returncode, 0,
                    "a permanent regression on a label the two parsers spell "
                    "differently exited 0:\n" + second.stderr)
                self.assertIn('coverage regressed', second.stderr)

    def test_no_committed_baseline_says_the_check_was_skipped(self):
        """A first run has nothing to compare against. It must say so rather
        than quietly falling back to whatever results.json is lying on disk --
        that fallback is what made a re-run launder a regression."""
        proc = self.run_survey(['ok-one'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('baseline: none committed', proc.stderr)
        self.assertIn('UNCHECKED', proc.stderr)


class BaselineReadFailures(SurveyFixture):
    """`git show` is load-bearing: it feeds the only real safety gate.

    "Nothing is committed yet" and "git could not be read" are different states
    and used to collapse into one exit-0 message, with `2>/dev/null` discarding
    the reason. A container running the survey over a bind-mounted checkout
    owned by another uid gets the second one routinely, and it printed
    'baseline: none committed' and ran with the regression check off.
    """

    def test_a_broken_object_store_is_fatal_not_an_unchecked_run(self):
        """Refs and repo layout intact, object CONTENT unreadable -- what git
        sees when the survey runs in a container over a bind-mounted checkout
        owned by another uid. Pre-fix this printed 'none committed' and exited 0
        with a real regression sitting on disk.

        Emptying the object files rather than deleting the directory is
        deliberate, and the difference matters: git's repository discovery
        requires `objects/` to EXIST, so removing it makes the tree stop being a
        git checkout at all -- a different state, already covered by
        test_a_non_git_checkout_is_a_legitimate_no_baseline. Corrupting content
        also needs no uid games, so it behaves the same for a root CI container.
        """
        self.assertEqual(self.run_survey(['ok-one', 'ok-two']).returncode, 0)
        self.commit_results()
        for obj in (self.root / '.git' / 'objects').rglob('*'):
            if obj.is_file():
                obj.chmod(0o644)      # git writes loose objects read-only
                obj.write_bytes(b'')
        # Precondition: git itself must now fail to read HEAD, or this test
        # proves nothing about the branch it is aimed at.
        probe = subprocess.run(
            ['git', '-C', str(self.root), 'ls-tree', '--full-tree', 'HEAD'],
            capture_output=True)
        self.assertNotEqual(probe.returncode, 0,
                            'fixture did not actually break the object store')

        self.write_manifest("ok-one | example/ok-one:1 |\n"
                            "ok-two | example/bad-run:1 |\n")
        proc = self.run_survey()
        self.assertNotEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('git could not read', proc.stderr)
        self.assertNotIn('none committed', proc.stderr)
        # The reason git gave is reported, not swallowed.
        self.assertNotIn('the coverage regression check will be skipped',
                         proc.stderr)

    def test_an_unborn_head_is_a_legitimate_no_baseline(self):
        """Non-vacuity for the check above: the fatal path must not swallow the
        genuine first-run case. A fresh `git init` has refs but no commit."""
        shutil.rmtree(self.root / '.git')
        self.git('init', '-q')
        proc = self.run_survey(['ok-one'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('none committed', proc.stderr)
        self.assertIn('has no commits yet', proc.stderr)
        self.assertIn('UNCHECKED', proc.stderr)

    def test_a_non_git_checkout_is_a_legitimate_no_baseline(self):
        """Second non-vacuity arm: an export with no .git at all (a release
        tarball) still runs, and still says the check was skipped."""
        shutil.rmtree(self.root / '.git')
        probe = subprocess.run(['git', '-C', str(self.root), 'rev-parse',
                                '--show-prefix'], capture_output=True)
        if probe.returncode == 0:
            self.skipTest('tmpdir sits inside an enclosing git repository')
        proc = self.run_survey(['ok-one'])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('none committed', proc.stderr)
        self.assertIn('is not a git checkout', proc.stderr)

    def test_git_is_preflighted_like_docker_and_python3(self):
        """git became a hard dependency the moment the baseline came out of
        HEAD, so a missing git must `die` up front rather than degrade into an
        exit-0 run with the regression check silently off."""
        farm = self.root / 'nogit-bin'
        farm.mkdir()
        needed = ['bash', 'mktemp', 'head', 'grep', 'rm', 'sleep', 'dirname',
                  'cat', 'sed', 'env', 'uname']
        for tool in needed:
            found = shutil.which(tool)
            if found:
                (farm / tool).symlink_to(found)
        (farm / 'python3').symlink_to(sys.executable)
        if shutil.which('git', path=str(farm)):
            self.skipTest('could not build a git-free PATH')
        # Sanity: the farm is usable at all, or a failure below means nothing.
        smoke = subprocess.run(['bash', '-c', 'python3 -c "print(1)"'],
                               env=dict(os.environ, PATH=str(farm)),
                               capture_output=True, text=True)
        if smoke.returncode != 0:
            self.skipTest(f'git-free PATH cannot run python3: {smoke.stderr}')

        proc = self.run_survey(['ok-one'], extra_env={'PATH': str(farm)})
        self.assertNotEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('git not found', proc.stderr)


class SweepAbortsThatUsedToDestroyTheEvidence(SurveyFixture):
    """Finding 2's residual: the render-first ordering only helps a run that
    REACHES render. Every abort between the first row and the render step left
    the previous healthy results.json byte-identical on disk and had cleanup()
    delete every skip collected so far -- the precise state the ordering claims
    to have removed."""

    def test_a_failed_skip_write_does_not_kill_the_run(self):
        """ENOSPC inside record_skip. The comment naming 'a full disk under
        PRUNE=1' was false for exactly this: record_skip is called unguarded
        under `set -euo pipefail`, so the write failure aborted the sweep before
        render and destroyed the artifact it promised."""
        shim = self.bin / 'py-shim' / 'python3'
        shim.parent.mkdir()
        shim.write_text(PY_ENOSPC_SHIM.replace('__REAL_PYTHON__',
                                               sys.executable))
        shim.chmod(0o755)
        env_path = f"{shim.parent}{os.pathsep}{os.environ.get('PATH', '')}"

        proc = self.run_survey(['ok-one', 'bad-pull', 'ok-two'],
                               extra_env={'PATH': env_path})

        # The artifact exists: the run stepped over the failed write.
        doc = self.results()
        self.assertEqual(doc['scenarioCount'], 2)
        self.assertEqual(doc['manifestRowCount'], 3)
        # The row could not be recorded, so it is unaccounted for -- and that
        # is LOUD, not a silently smaller denominator.
        self.assertEqual(doc['skippedCount'], 0)
        self.assertNotEqual(proc.returncode, 0, proc.stderr)
        self.assertIn('could not be recorded', proc.stderr)
        self.assertIn('unaccounted for', proc.stderr)

    def test_an_unbalanced_quote_in_a_row_no_longer_aborts_the_sweep(self):
        """`label="$(echo "$label" | xargs)"` had no `|| true` while the flags
        line did. One unbalanced quote anywhere in images.txt was `xargs:
        unterminated quote`, a non-zero exit mid-sweep, and the loss of every
        skip recorded before it plus every row after it."""
        self.write_manifest('bad-pull-row | example/bad-pull:1 |\n'
                            'say "hi | example/ok-one:1 |\n'
                            'after | example/ok-two:1 |\n')
        proc = self.run_survey()
        self.assertEqual(proc.returncode, 0, proc.stderr)
        doc = self.results()
        self.assertEqual(doc['manifestRowCount'], 3)
        # The quote is data. The row before it kept its skip record, and the
        # row after it was still swept.
        self.assertIn('say "hi', [s['label'] for s in doc['scenarios']])
        self.assertIn('after', [s['label'] for s in doc['scenarios']])
        self.assertEqual([s['label'] for s in doc['skipped']],
                         ['bad-pull-row'])


class RefreshWorkflowKeepsFailedEvidence(unittest.TestCase):
    """The CI arm of the durability claim.

    survey.sh writing results.* before the guard buys nothing in CI unless
    something collects them: on a red run every downstream step (regenerate,
    commit, push, open PR) is skipped, so the files die with the runner and the
    expiring Actions log is again the only record. Asserted here rather than
    only described, because an invariant stated in a PR body and checked nowhere
    is decoration.
    """

    WORKFLOW = REPO_ROOT / '.github' / 'workflows' / 'scores-refresh.yml'

    def setUp(self):
        self.text = self.WORKFLOW.read_text()

    def test_the_survey_artifact_is_uploaded_even_when_the_run_fails(self):
        self.assertIn('actions/upload-artifact@', self.text)
        upload = self.text.index('actions/upload-artifact@')
        step = self.text.rindex('- name:', 0, upload)
        block = self.text[step:upload + 600]
        self.assertIn('if: always()', block,
                      "the upload must run on the failing run, which is the "
                      "only run whose artifact is otherwise lost")
        self.assertIn('examples/isolation-survey/results.json', block)
        self.assertIn('examples/isolation-survey/results.md', block)

    def test_the_upload_runs_before_anything_that_could_skip_it(self):
        """It has to sit directly after the survey step. Placed after the
        regenerate/build steps it would be skipped along with them on a failure
        of one of those, which is the same hole one step further down."""
        survey = self.text.index('bash examples/isolation-survey/survey.sh')
        upload = self.text.index('actions/upload-artifact@')
        # ...the first mention AFTER the survey step: the file also names
        # gen_scorecards.py in its header comment.
        regen = self.text.index('gen_scorecards.py', survey)
        self.assertLess(survey, upload)
        self.assertLess(upload, regen)


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

    def run_render(self, labels, manifest_rows, skips=()):
        """Returns (CompletedProcess, dir). `manifest_rows=None` omits the flag
        entirely, which is the shape that used to synthesize a denominator."""
        records = [{"label": lab, "image": f"example/{lab}:1",
                    "runFlags": "", "resolvedDigest": "",
                    "report": {"score": 42, "grade": "D",
                               "generatedAt": "2026-01-01T00:00:00Z",
                               "version": "test", "dimensions": []}}
                   for lab in labels]
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        d = pathlib.Path(tmp.name)
        argv = [str(SURVEY_DIR / 'render.py'),
                str(d / 'results.json'), str(d / 'results.md')]
        if manifest_rows is not None:
            argv += ['--manifest-rows', str(manifest_rows)]
        if skips:
            (d / 'skips.json').write_text(json.dumps(
                [{"label": lab, "image": f"example/{lab}:1",
                  "stage": stage, "reason": reason}
                 for lab, stage, reason in skips]))
            argv += ['--skips', str(d / 'skips.json')]
        proc = subprocess.run([sys.executable, *argv],
                              input=json.dumps(records),
                              capture_output=True, text=True)
        return proc, d

    def render(self, labels, manifest_rows, skips=()):
        proc, d = self.run_render(labels, manifest_rows, skips)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        return (d / 'results.md').read_text()

    def test_a_missing_manifest_row_count_is_refused_not_synthesized(self):
        """The denominator used to default to `len(rows) + len(skips)`, so the
        coverage invariant held by construction: one scored row and no skips
        wrote `manifestRowCount: 1` and printed 'Coverage: 1 of 1 manifest rows
        - every row was scanned' against a 295-row manifest. A count nobody
        measured is not a count."""
        proc, d = self.run_render(['a'], manifest_rows=None)
        self.assertNotEqual(proc.returncode, 0,
                            "render.py accepted a run with no measured "
                            "denominator")
        self.assertIn('manifest-rows', proc.stderr)
        self.assertFalse((d / 'results.json').exists())
        self.assertFalse((d / 'results.md').exists())

    def test_full_coverage_is_not_claimed_when_skips_over_account(self):
        """Every manifest row scored AND a skip was recorded: the counts do not
        close, so the artifact must report that rather than print the happy
        sentence and drop the skip table.

        A spec test, NOT a non-vacuity anchor: it passes against the pre-fix
        file too, because `len(rows) == manifest_rows and not skips` reaches the
        same branch here. It is kept to pin the behaviour while the condition is
        rewritten in terms of the three counts. The anchor for that rewrite is
        test_a_missing_manifest_row_count_is_refused_not_synthesized.
        """
        md = self.render(['a', 'b'], manifest_rows=2,
                         skips=[('c', 'pull', 'manifest unknown')])
        self.assertNotIn('every row was scanned', md)
        self.assertIn('unaccounted for', md)
        self.assertIn('## Not scanned', md)

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
