#!/usr/bin/env python3
"""Regression tests for the two fail-quiet defects in scripts/measure-cron-latency.py.

Both defects (IRO-685) fail in the SUPPRESSING direction: they cannot manufacture a
finding, they erase one. That makes them invisible to a smoke test that only checks the
script runs, so each gets an explicit test that asserts on what the output must NOT say.

Run:
    python3 -m unittest discover -s scripts/tests -v

Manual reproduction of the same two cases against the live API, for the record:

  Defect 2 (a failed call rendered as "never fired on a schedule"):
      GH_TOKEN=invalid gh auth logout --hostname github.com 2>/dev/null; \
      GH_TOKEN=invalid scripts/measure-cron-latency.py; echo "exit=$?"
  Before the fix: every row reads `0 runs / 0 gaps` and `(never fired on a schedule)`,
  the pooled block is absent, and exit=0. After: every row reads
  `MEASUREMENT FAILED: gh exit 1: ... HTTP 401 ...` and exit=1.

  Defect 1 (one stuck historical run pins in_flight forever):
      gh api "repos/IronSecCo/ironclaw/actions/workflows/brew-bump-waiting.yml/runs\
?event=schedule&per_page=100" --jq '.workflow_runs[] | "\\(.created_at) \\(.status)"'
  Any line whose status is not `completed` used to pin the silence row to
  `NOT SCORED (a run is in flight)`. Runs parked in `waiting` awaiting approval are this
  repo's normal state, so that was the expected case, not an edge case. After the fix
  only the newest line's status can suppress the row.
"""

from __future__ import annotations

import contextlib
import datetime as dt
import importlib.util
import io
import os
import pathlib
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

_SCRIPT = pathlib.Path(__file__).resolve().parent.parent / "measure-cron-latency.py"

# A fixed date safely in the past, so the live-silence figure the end-to-end tests
# exercise is a large positive number that does not depend on when the suite runs.
_PAST_COMPLETED = "2025-01-02T14:00:00Z completed\n"


def _load() -> types.ModuleType:
    """Import the script as a module. The filename has a dash, so it is not importable
    by name; loading it by path keeps the script executable as a script."""
    spec = importlib.util.spec_from_file_location("measure_cron_latency", _SCRIPT)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    # Registered before exec: @dataclasses.dataclass resolves its class's module out of
    # sys.modules, and blows up on None if the module is not there yet.
    sys.modules[spec.name] = mod
    spec.loader.exec_module(mod)
    return mod


mcl = _load()


def _completed(returncode: int = 0, stdout: str = "", stderr: str = ""):
    return subprocess.CompletedProcess(args=["gh"], returncode=returncode,
                                       stdout=stdout, stderr=stderr)


def _fake_gh(runs_result, state_result):
    """A subprocess.run stand-in that answers the two calls the script makes. The runs
    call is the one carrying `/runs?`; anything else is the workflow-state call."""

    def run(cmd, *_a, **_kw):
        return runs_result if any("/runs?" in str(part) for part in cmd) else state_result

    return run


class InFlightIsAboutTheNewestRunOnly(unittest.TestCase):
    """Defect 1: `in_flight` was an OR over the entire run history."""

    def test_stuck_historical_run_does_not_pin_in_flight(self):
        # Newest run (14:00) completed; an older one is parked in `waiting` forever --
        # the repo's normal state, since runs park awaiting approval.
        stdout = ("2026-07-30T14:00:00Z completed\n"
                  "2026-07-29T14:00:00Z waiting\n"
                  "2026-07-28T14:00:00Z completed\n")
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(stdout=stdout), _completed())):
            runs, in_flight, err = mcl._runs("o/n", "wf.yml")
        self.assertIsNone(err)
        self.assertFalse(in_flight, "a stuck HISTORICAL run must not report as in flight")
        self.assertEqual(len(runs), 3)
        # Oldest first, regardless of the newest-first order the API returns.
        self.assertEqual(runs, sorted(runs))
        self.assertEqual(runs[-1].hour, 14)
        self.assertEqual(runs[-1].day, 30)

    def test_newest_run_in_flight_still_reports(self):
        # The flag must keep working for what it is actually for: the scheduler has
        # fired for the current interval and we are only waiting on a runner.
        stdout = ("2026-07-30T14:00:00Z queued\n"
                  "2026-07-29T14:00:00Z completed\n")
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(stdout=stdout), _completed())):
            _runs, in_flight, err = mcl._runs("o/n", "wf.yml")
        self.assertIsNone(err)
        self.assertTrue(in_flight)

    def test_no_runs_is_not_in_flight(self):
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(stdout=""), _completed())):
            runs, in_flight, err = mcl._runs("o/n", "wf.yml")
        self.assertEqual(runs, [])
        self.assertFalse(in_flight)
        self.assertIsNone(err, "an empty history is a real measurement, not a failure")


class FailedCallIsNotAnObservation(unittest.TestCase):
    """Defect 2: a non-zero `gh` exit was returned as an empty run history."""

    def test_runs_failure_returns_a_reason_not_an_empty_history(self):
        err_text = "gh: Bad credentials (HTTP 401)"
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(1, stderr=err_text), _completed())):
            runs, in_flight, err = mcl._runs("o/n", "wf.yml")
        self.assertEqual(runs, [])
        self.assertFalse(in_flight)
        self.assertIsNotNone(err)
        # The reason must distinguish an auth failure from an empty history.
        self.assertIn("401", err)
        self.assertIn("Bad credentials", err)

    def test_partial_paginate_output_is_discarded_on_failure(self):
        # `--paginate` can fail on a later page with earlier pages already on stdout.
        # A truncated history reports a healthier cadence than reality, so it is dropped.
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(1, stdout="2026-07-30T14:00:00Z completed\n",
                                                   stderr="API rate limit exceeded"),
                                        _completed())):
            runs, _in_flight, err = mcl._runs("o/n", "wf.yml")
        self.assertEqual(runs, [])
        self.assertIn("rate limit", err)

    def test_state_failure_is_distinct_from_a_state_value(self):
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(), _completed(1, stderr="HTTP 403"))):
            state, err = mcl._state("o/n", "wf.yml")
        self.assertEqual(state, "unknown")
        self.assertIn("403", err)

    def test_state_success_carries_no_error(self):
        with mock.patch.object(mcl.subprocess, "run",
                               _fake_gh(_completed(), _completed(stdout="active\n"))):
            state, err = mcl._state("o/n", "wf.yml")
        self.assertEqual(state, "active")
        self.assertIsNone(err)

    def test_reason_is_one_line_and_bounded(self):
        proc = _completed(1, stderr="a\nb\n" + "x" * 500)
        reason = mcl._gh_failure(proc)
        self.assertNotIn("\n", reason)
        self.assertLessEqual(len(reason), mcl._MAX_REASON + len("gh exit 1: "))

    def test_empty_stderr_still_yields_a_reason(self):
        self.assertIn("no stderr", mcl._gh_failure(_completed(7)))


class EndToEndOutput(unittest.TestCase):
    """The tables and exit code, over this repo's real `schedule:` workflows with every
    `gh` call stubbed. Asserts on what the output must not say."""

    def _main(self, runs_result, state_result, argv=("measure-cron-latency.py",)):
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(mcl.subprocess, "run", _fake_gh(runs_result, state_result)), \
                mock.patch.object(sys, "argv", list(argv)), \
                contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = mcl.main()
        return code, out.getvalue(), err.getvalue()

    def test_repo_has_scheduled_workflows_to_measure(self):
        # Guards the tests below from becoming vacuous: if nothing in this repo parses as
        # a single-cron scheduled workflow, the end-to-end assertions prove nothing.
        _code, out, _err = self._main(_completed(stdout=_PAST_COMPLETED),
                                      _completed(stdout="active"))
        self.assertIn(".yml", out, "no scheduled workflow rows were produced")

    def test_total_gh_failure_reports_failure_not_never_fired(self):
        code, out, err = self._main(_completed(1, stderr="gh: Bad credentials (HTTP 401)"),
                                    _completed(1, stderr="gh: Bad credentials (HTTP 401)"))
        self.assertIn("MEASUREMENT FAILED", out)
        self.assertNotIn("never fired on a schedule", out,
                         "a failed call must never print as a claim about the scheduler")
        self.assertNotIn("0 runs", out)
        self.assertNotIn("POOLED", out, "a failed measurement must not reach the pool")
        self.assertIn("could not be measured", err)
        self.assertEqual(code, 1, "a failed measurement must exit non-zero")

    def test_clean_measurement_exits_zero(self):
        code, out, err = self._main(_completed(stdout=_PAST_COMPLETED),
                                    _completed(stdout="active"))
        self.assertEqual(code, 0, err)
        self.assertNotIn("MEASUREMENT FAILED", out)

    def test_stuck_historical_run_still_scores_silence(self):
        stdout = _PAST_COMPLETED + "2025-01-01T14:00:00Z waiting\n"
        code, out, _err = self._main(_completed(stdout=stdout), _completed(stdout="active"))
        self.assertEqual(code, 0)
        self.assertNotIn("is in flight", out,
                         "a stuck historical run must not suppress the silence row")

    def test_state_failure_does_not_masquerade_as_an_exclusion(self):
        # `unknown` is not `active`, so before the fix this printed the same
        # "NOT SCORED (state: ...)" as a deliberately-skipped disabled workflow.
        code, out, _err = self._main(_completed(stdout=_PAST_COMPLETED),
                                     _completed(1, stderr="HTTP 403"))
        self.assertIn("MEASUREMENT FAILED (state unknown)", out)
        self.assertNotIn("NOT SCORED (state: unknown)", out)
        self.assertEqual(code, 1)

    def test_phase_flag_skips_failed_measurements(self):
        code, _out, _err = self._main(_completed(1, stderr="HTTP 403"),
                                      _completed(stdout="active"),
                                      argv=("measure-cron-latency.py", "--phase"))
        self.assertEqual(code, 1)


class ArithmeticIsUnchanged(unittest.TestCase):
    """IRO-685 explicitly must not alter the measurement arithmetic. These pin the
    `_period_minutes` answers the published 135 min / 7.9x figures were derived from."""

    def test_period_minutes(self):
        for cron, expected in [
            ("*/30 * * * *", 30),
            ("13,43 * * * *", 30),
            ("17 * * * *", 60),
            ("41 6 * * *", 1440),
            ("23 7 * * 1", 10080),
            ("0 6 * * 1,4", None),
            ("0 6 1 * *", None),
            ("nonsense", None),
        ]:
            with self.subTest(cron=cron):
                self.assertEqual(mcl._period_minutes(cron), expected)

    def test_schedule_block_with_no_parsable_cron_still_raises(self):
        # The file's own standard: refuse to under-report rather than drop a workflow.
        with tempfile.TemporaryDirectory() as d:
            bad = pathlib.Path(d) / "bad.yml"
            bad.write_text("on:\n  schedule:\n    - notacron: x\n")
            with self.assertRaises(SystemExit):
                mcl._crons(bad)


def _wf(cron: str) -> str:
    return f'name: w\non:\n  schedule:\n    - cron: "{cron}"\njobs: {{}}\n'


class CronChangeBoundsTheMeasurementWindow(unittest.TestCase):
    """IRO-680: runs delivered under a REPLACED cron were scored and then labelled with
    the cron currently in the file.

    This is not a fail-quiet defect like the two above -- it manufactures a finding
    rather than erasing one, and it does so with a confident-looking sample. Live case:
    `fork-ci-approval-backstop` moved `*/30` -> `13,43`, and the row reported
    `4 runs / 3 gaps / p50 +129 / max +299` for `13,43` when every one of those gaps was
    delivered by `*/30`. The offset under test had produced no complete interval at all."""

    @staticmethod
    def _repo(commits: list[tuple[str, str]]) -> tempfile.TemporaryDirectory:
        """A git repo whose workflow file takes each (cron, ISO-8601 date) in turn."""
        tmp = tempfile.TemporaryDirectory()
        root = pathlib.Path(tmp.name)
        wf = root / ".github" / "workflows"
        wf.mkdir(parents=True)
        env = {
            **os.environ,
            "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@e",
            "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@e",
        }
        subprocess.run(["git", "-C", str(root), "init", "-q"], check=True, env=env)
        for cron, when in commits:
            (wf / "w.yml").write_text(_wf(cron))
            subprocess.run(["git", "-C", str(root), "add", "-A"], check=True, env=env)
            subprocess.run(
                # --allow-empty: an "unchanged cron" case commits an identical file, and
                # that touching-but-not-changing commit is exactly what the walk must
                # step over to find where the cron really landed.
                ["git", "-C", str(root), "commit", "-q", "--allow-empty", "-m", cron],
                check=True, env={**env, "GIT_AUTHOR_DATE": when, "GIT_COMMITTER_DATE": when},
            )
        return tmp

    _REL = pathlib.Path(".github/workflows/w.yml")

    def test_returns_when_the_current_cron_landed_not_the_first_commit(self):
        with self._repo([("*/30 * * * *", "2026-07-29T00:00:00+00:00"),
                         ("13,43 * * * *", "2026-07-30T04:49:11+00:00")]) as d:
            landed = mcl._cron_landed_at(pathlib.Path(d), self._REL, ["13,43 * * * *"])
        self.assertIsNotNone(landed, "a cron change in full history must be locatable")
        # The replacement's landing time, NOT the file's first commit.
        self.assertEqual(landed.astimezone(dt.timezone.utc).isoformat(),
                         "2026-07-30T04:49:11+00:00")

    def test_pre_change_runs_fall_outside_the_window(self):
        # The actual regression: three runs under the old cron, one under the new. The
        # window must admit only the last, leaving zero gaps -- not three.
        with self._repo([("*/30 * * * *", "2026-07-29T00:00:00+00:00"),
                         ("13,43 * * * *", "2026-07-30T04:49:11+00:00")]) as d:
            landed = mcl._cron_landed_at(pathlib.Path(d), self._REL, ["13,43 * * * *"])
        runs = [dt.datetime.fromisoformat(t) for t in (
            "2026-07-29T22:28:16+00:00", "2026-07-29T23:33:53+00:00",
            "2026-07-30T02:12:42+00:00", "2026-07-30T07:41:30+00:00")]
        # The counterfactual, pinned so this test cannot go vacuous: unbounded, these
        # same four timestamps yield the three gaps that were published as `13,43`.
        self.assertEqual(len(list(zip(runs, runs[1:]))), 3)
        kept = [r for r in runs if r >= landed]
        self.assertEqual(len(kept), 1, "only the post-change run measures this cron")
        self.assertEqual(len(list(zip(kept, kept[1:]))), 0,
                         "one run is zero intervals; there is nothing to report yet")

    def test_unchanged_cron_keeps_its_whole_history(self):
        # No change means no exclusions: the bound must not quietly shrink a good sample.
        with self._repo([("17 3 * * *", "2026-07-01T00:00:00+00:00"),
                         ("17 3 * * *", "2026-07-20T00:00:00+00:00")]) as d:
            landed = mcl._cron_landed_at(pathlib.Path(d), self._REL, ["17 3 * * *"])
        self.assertEqual(landed.astimezone(dt.timezone.utc).isoformat(),
                         "2026-07-01T00:00:00+00:00")

    def test_no_git_history_does_not_bound_the_window(self):
        # Fail open. An undeterminable window must not masquerade as a narrow one, which
        # would silently discard every run and report a healthy cron as never firing.
        with tempfile.TemporaryDirectory() as d:
            root = pathlib.Path(d)
            (root / ".github" / "workflows").mkdir(parents=True)
            (root / ".github" / "workflows" / "w.yml").write_text(_wf("*/30 * * * *"))
            self.assertIsNone(
                mcl._cron_landed_at(root, self._REL, ["*/30 * * * *"]),
                "outside a git repo the script must measure everything, not nothing")

    def test_history_and_worktree_share_one_parser(self):
        # A second parser for historical blobs could report a cron change that never
        # happened, or miss one that did.
        text = _wf("13,43 * * * *")
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d) / "w.yml"
            p.write_text(text)
            self.assertEqual(mcl._crons(p), mcl._crons_from_text(text, "w.yml"))


if __name__ == "__main__":
    unittest.main()
