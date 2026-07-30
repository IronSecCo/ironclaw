#!/usr/bin/env python3
"""Tests for scripts/check-brew-bump-waiting.sh, and specifically for arm B (IRO-690).

WHY THESE EXIST
---------------
The guard's whole job is to go RED, and until IRO-690 it had exactly one arm, which
discovered work by listing open `brew/track` PRs and exited 0 when there were none. So
the tap could be serving an older release than the newest one with zero open bump PRs
and the guard would say "not waiting on a click" and pass. That is what IRO-689
produced: GitHub auto-closed #636 when its head became == its base, and between 10:39Z
and ~11:19Z on 2026-07-30 the tap served v0.1.447 against a published v0.1.450.

Arm B measures the user-visible property instead — `version` in Formula/ironclaw.rb on
main versus `releases/latest` — so it needs no PR to exist. Per the IRO-685 standing
rule, a guard only ever observed green is indistinguishable from `exit 0`, so the
load-bearing test here is test_stale_tap_with_no_open_pr_is_red: it drives the script
with the real IRO-689 pair (formula 0.1.447, release v0.1.450) and asserts non-zero.

PROOF THAT THAT TEST IS NON-VACUOUS
-----------------------------------
Asserting red against the FIXED script only shows the fixed script is red. The pre-fix
file has to be shown GREEN on the same input, or the test is compatible with the arm
never having been added. Reproduction, run against the pre-IRO-690 blob:

    git show <pre-IRO-690-sha>:scripts/check-brew-bump-waiting.sh > /tmp/prefix.sh
    chmod +x /tmp/prefix.sh
    python3 -m unittest scripts.tests.test_check_brew_bump_waiting \
        -k stale_tap_with_no_open_pr   # with SCRIPT pointed at /tmp/prefix.sh

Recorded result: the pre-fix script prints "No open brew/track PR. The tap is not
waiting on a click." and exits 0 on the exact fixture test_stale_tap_with_no_open_pr_is_red
uses. The test fails against it (expected 1, got 0) and passes against the fixed file.

THE SAME RED, WITH NOTHING STUBBED
----------------------------------
Arm B's red path is also reproducible against the LIVE API, because TRACK_BRANCH accepts
any git ref — including the commit where the formula really did say 0.1.447. Recorded
2026-07-30T11:54Z, with v0.1.450 the live newest release:

    TRACK_BRANCH=495269e8f09510fd6761857b0dfb3aad57c95826 \
    STALE_THRESHOLD_MINUTES=10 scripts/check-brew-bump-waiting.sh; echo "exit=$?"

  ::error::The Homebrew tap is STALE: the tap has been serving 0.1.447 for 76m while
  v0.1.450 is the newest release (threshold 10m). ...
  Open brew/track PRs: 0
  ZERO open brew/track PRs: nothing is even trying to fix this, which is the IRO-689 shape.
  exit=1

Both readings are real: a real formula blob, the real releases/latest, zero real open bump
PRs. The stubbed tests below exist to pin the cases that cannot be conjured from live state
(a formula ahead of latest, an unreadable endpoint, a draft "latest").

HOW THE STUB WORKS
------------------
`gh` is replaced by a shell script earlier on PATH. It answers the four calls the guard
makes from fixture files, and it pipes fixture JSON through the REAL `jq` when the guard
passed `--jq`, so the guard's own jq filters are exercised rather than bypassed. It also
appends every argv it sees to a log, which is how test_arm_b_reads_the_track_branch_ref
pins that arm B measures the tap's branch and not whatever ref is checked out.

Run:
    python3 -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import textwrap
import time
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / "check-brew-bump-waiting.sh"
REPO = "IronSecCo/ironclaw"

# The two versions from the real IRO-689 outage. Used deliberately instead of made-up
# numbers so the fixture is the outage rather than a rhyme with it.
IRO689_FORMULA_VERSION = "0.1.447"
IRO689_LATEST_TAG = "v0.1.450"

_FORMULA_TEMPLATE = textwrap.dedent(
    """\
    # typed: false
    # frozen_string_literal: true
    class Ironclaw < Formula
      desc "Security-hardened, self-hosted AI assistant platform (secured Go port)"
      homepage "https://github.com/IronSecCo/ironclaw"
      version "{version}"
      license "AGPL-3.0-or-later"
    end
    """
)

# A `gh` stand-in. Dispatches on argv, answers from $STUB_DIR, and applies the caller's
# own --jq filter with real jq so the guard's filters are under test too.
_STUB_GH = r"""#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >> "${STUB_DIR}/argv.log"

# fixture <name> [<jq-filter-arg-index-scan...>]: emit $STUB_DIR/<name>, honouring an
# optional <name>.rc file that forces a non-zero exit (an API failure).
emit() {
  local name="$1"; shift
  if [ -f "${STUB_DIR}/${name}.rc" ]; then
    printf 'stub: forced failure for %s\n' "$name" >&2
    exit "$(cat "${STUB_DIR}/${name}.rc")"
  fi
  local filter=""
  while [ $# -gt 0 ]; do
    if [ "$1" = "--jq" ]; then filter="$2"; break; fi
    shift
  done
  if [ -n "$filter" ]; then
    jq -r "$filter" < "${STUB_DIR}/${name}"
  else
    cat "${STUB_DIR}/${name}"
  fi
}

case "$1" in
  pr)
    case "$2" in
      list) emit pr_list.json "$@" ;;
      view) emit pr_view.json "$@" ;;
      *) printf 'stub: unhandled gh pr %s\n' "$2" >&2; exit 64 ;;
    esac
    ;;
  api)
    case "$*" in
      *releases/latest*) emit release.json "$@" ;;
      *contents/*)       emit formula.rb "$@" ;;
      *) printf 'stub: unhandled gh api %s\n' "$*" >&2; exit 64 ;;
    esac
    ;;
  *) printf 'stub: unhandled gh %s\n' "$1" >&2; exit 64 ;;
esac
"""


def _iso(minutes_ago: int) -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - minutes_ago * 60))


class GuardHarness:
    """A temp $STUB_DIR pre-loaded with an all-green fixture set, plus a runner."""

    def __init__(self, tmp: pathlib.Path) -> None:
        self.dir = tmp / "stub"
        self.dir.mkdir()
        bindir = tmp / "bin"
        bindir.mkdir()
        gh = bindir / "gh"
        gh.write_text(_STUB_GH)
        gh.chmod(0o755)
        self.bindir = bindir

        # Default: no open bump PRs, and the tap in sync with a release cut 10h ago.
        self.set_open_prs([])
        self.set_release("v0.1.450", published_minutes_ago=600)
        self.set_formula_version("0.1.450")

    # --- fixture setters -------------------------------------------------
    def set_open_prs(self, prs: list[dict]) -> None:
        (self.dir / "pr_list.json").write_text(json.dumps(prs))

    def set_open_pr_age(self, number: int, minutes_ago: int, *, draft: bool = False) -> None:
        self.set_open_prs(
            [
                {
                    "number": number,
                    "createdAt": _iso(minutes_ago),
                    "isDraft": draft,
                    "headRefOid": "deadbeefcafe0000",
                    "url": f"https://github.com/{REPO}/pull/{number}",
                }
            ]
        )

    def set_check_rollup(self, entries: list[dict]) -> None:
        (self.dir / "pr_view.json").write_text(json.dumps({"statusCheckRollup": entries}))

    def set_release(self, tag: str, *, published_minutes_ago: int, draft: bool = False,
                    prerelease: bool = False) -> None:
        (self.dir / "release.json").write_text(
            json.dumps(
                {
                    "tag_name": tag,
                    "published_at": _iso(published_minutes_ago),
                    "draft": draft,
                    "prerelease": prerelease,
                }
            )
        )

    def set_formula_version(self, version: str) -> None:
        (self.dir / "formula.rb").write_text(_FORMULA_TEMPLATE.format(version=version))

    def set_formula_source(self, text: str) -> None:
        (self.dir / "formula.rb").write_text(text)

    def force_failure(self, fixture: str, rc: int = 1) -> None:
        (self.dir / f"{fixture}.rc").write_text(str(rc))

    @property
    def argv_log(self) -> str:
        path = self.dir / "argv.log"
        return path.read_text() if path.exists() else ""

    # --- runner ----------------------------------------------------------
    def run(self, *, script: pathlib.Path | None = None,
            **env_overrides: str) -> subprocess.CompletedProcess[str]:
        env = dict(os.environ)
        env["PATH"] = f"{self.bindir}{os.pathsep}{env['PATH']}"
        env["STUB_DIR"] = str(self.dir)
        env["REPO"] = REPO
        env.update(env_overrides)
        return subprocess.run(
            [str(script or SCRIPT)], capture_output=True, text=True, env=env, timeout=120
        )


def _mutant(drop_pattern: str, suffix: str) -> pathlib.Path:
    """A copy of the guard with every line matching `drop_pattern` removed.

    Proves a defence is load-bearing without waiting for someone to reintroduce the
    defect for real: delete what the defence protects against and the defence must fire.
    Written next to the real script because the guard derives `${ROOT}` from `$0`, so a
    copy anywhere else would read a different (missing) ruleset and fail for the wrong
    reason. The caller unlinks it.
    """
    src = SCRIPT.read_text().splitlines(keepends=True)
    kept = [line for line in src if not re.search(drop_pattern, line)]
    if len(kept) == len(src):
        raise AssertionError(
            f"mutation pattern {drop_pattern!r} matched nothing in {SCRIPT}; the test it "
            "backs would pass against an unmutated script and prove nothing"
        )
    out = SCRIPT.parent / f".mutant-{suffix}.sh"
    out.write_text("".join(kept))
    out.chmod(0o755)
    return out


class BrewBumpWaitingTest(unittest.TestCase):
    def setUp(self) -> None:
        if shutil.which("jq") is None:  # pragma: no cover - jq is a hard dep of the guard
            self.skipTest("jq is required by the guard and by the gh stub")
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.h = GuardHarness(pathlib.Path(self._tmp.name))

    def assertRed(self, proc: subprocess.CompletedProcess[str]) -> str:
        self.assertEqual(
            proc.returncode,
            1,
            f"expected the guard to go RED.\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}",
        )
        return proc.stderr

    def assertGreen(self, proc: subprocess.CompletedProcess[str]) -> str:
        self.assertEqual(
            proc.returncode,
            0,
            f"expected the guard to be GREEN.\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}",
        )
        return proc.stdout

    # -----------------------------------------------------------------
    # Arm B — the IRO-690 gap.
    # -----------------------------------------------------------------
    def test_stale_tap_with_no_open_pr_is_red(self) -> None:
        """The load-bearing case. Pinned to the real IRO-689 pair, with the release old
        enough to be past the threshold. Zero open bump PRs, so arm A has nothing to say
        and the pre-fix script exited 0 right here."""
        self.h.set_open_prs([])
        self.h.set_formula_version(IRO689_FORMULA_VERSION)
        self.h.set_release(IRO689_LATEST_TAG, published_minutes_ago=500)

        err = self.assertRed(self.h.run())
        self.assertIn("tap is STALE", err)
        self.assertIn(IRO689_FORMULA_VERSION, err)
        self.assertIn(IRO689_LATEST_TAG, err)
        # The zero-PR branch of the remediation is the IRO-689 shape and must be the one
        # shown: telling someone to merge a PR that does not exist is not a next action.
        self.assertIn("ZERO open brew/track PRs", err)
        self.assertIn("auto-closes a PR whose head becomes == its base", err)
        self.assertIn(f"update-homebrew-formula.sh {IRO689_LATEST_TAG}", err)
        self.assertNotIn("A bump PR IS open", err)

    def test_fresh_tap_with_no_open_pr_is_green(self) -> None:
        """The other half of the discrimination proof: same code path, same absence of
        PRs, in-sync formula => green. Red-on-stale only means something next to this."""
        self.h.set_formula_version("0.1.450")
        self.h.set_release("v0.1.450", published_minutes_ago=500)
        out = self.assertGreen(self.h.run())
        self.assertIn("Tap is in sync with the newest release", out)
        self.assertIn("Both arms clear", out)
        # Only in THIS branch may the summary claim the newest release is served.
        self.assertIn("the tap serves the newest release (0.1.450)", out)

    def test_stale_tap_inside_the_threshold_is_green(self) -> None:
        """The IRO-689 window as actually observed: ~40 min of trail. The README
        advertises that the tap "can briefly trail", so green here is correct — arm B
        bounds the window, it does not forbid one."""
        self.h.set_formula_version(IRO689_FORMULA_VERSION)
        self.h.set_release(IRO689_LATEST_TAG, published_minutes_ago=40)
        out = self.assertGreen(self.h.run())
        self.assertIn("Inside the advertised brief-trail window", out)

    def test_green_summary_does_not_claim_a_trailing_tap_is_in_sync(self) -> None:
        """The load-bearing negative for the SUMMARY line (IRO-694). Arm B is allowed to
        be green while the tap trails inside the grace window, but the summary must then
        say so rather than assert the opposite one line below arm B's own finding.

        NON-VACUITY: this is a suppressing/report-half defect, so it passes against the
        fixed script by construction and proves nothing on its own. Run it against the
        pre-fix file (`git show 84e7594:scripts/check-brew-bump-waiting.sh`) and it FAILS
        on the assertNotIn: that summary hardcoded "the tap serves the newest release"
        and printed it verbatim under a trailing tap. Observed live in run 30546035595,
        which reported the tap on 0.1.450 against a published v0.1.452 and then called
        itself in sync. Same rule as IRO-676's "an honest green must ENUMERATE a real
        row", applied to the verdict instead of the discovery."""
        self.h.set_formula_version(IRO689_FORMULA_VERSION)
        self.h.set_release(IRO689_LATEST_TAG, published_minutes_ago=40)
        out = self.assertGreen(self.h.run())
        self.assertIn("Both arms clear", out)
        self.assertNotIn("the tap serves the newest release", out)
        self.assertIn(f"the tap trails {IRO689_LATEST_TAG} by 40m", out)

    def test_green_verdict_refuses_to_print_without_an_arm_b_finding(self) -> None:
        """IRO-693. Carrying arm B's finding into the verdict fixed the wording for the
        two branches that exist today; it did not stop a THIRD branch from being added
        that fills neither TAP_VERDICT nor STALE. That case still reaches the all-clear,
        and an empty interpolation reads as a well-formed green ("...waiting past 240m,
        and . Tap freshness is within the advertised window.") to anyone skimming the
        last line — the same overclaim, one level down and harder to see.

        Driven by MUTATION rather than by a fixture, because no input can reach that
        state on today's four branches: strip the in-branch `TAP_VERDICT=` assignments
        and run the otherwise-green in-sync fixture. Without the assertion this mutant
        exits 0 with the empty sentence; with it, it exits 1 naming the cause."""
        mutant = _mutant(r'^\s+TAP_VERDICT=', "no-tap-verdict")
        self.addCleanup(mutant.unlink)

        self.h.set_formula_version("0.1.450")
        self.h.set_release("v0.1.450", published_minutes_ago=500)
        proc = self.h.run(script=mutant)

        self.assertEqual(
            proc.returncode,
            1,
            "a green verdict with no arm-B finding behind it must be RED.\n"
            f"stdout:\n{proc.stdout}\nstderr:\n{proc.stderr}",
        )
        self.assertIn("arm B finished without recording what it found", proc.stderr)
        self.assertNotIn("Both arms clear", proc.stdout)

    def test_stale_threshold_is_independently_tunable(self) -> None:
        """Same 40-minute trail goes red under a tighter arm-B threshold, and arm A's
        threshold does not silently drive arm B."""
        self.h.set_formula_version(IRO689_FORMULA_VERSION)
        self.h.set_release(IRO689_LATEST_TAG, published_minutes_ago=40)
        err = self.assertRed(self.h.run(STALE_THRESHOLD_MINUTES="10"))
        self.assertIn("threshold 10m", err)

    def test_formula_ahead_of_latest_release_is_red_with_no_grace(self) -> None:
        """A formula pinned past releases/latest means its download URLs point at a
        release that was deleted, unpublished, or demoted. Time does not heal that, so
        it fires even though the release was published one minute ago."""
        self.h.set_formula_version("0.1.451")
        self.h.set_release("v0.1.450", published_minutes_ago=1)
        err = self.assertRed(self.h.run())
        self.assertIn("NEWER than the newest release", err)

    def test_arm_b_reads_the_track_branch_ref(self) -> None:
        """Arm B must measure the branch `brew tap` serves, not the working copy. The
        guard also runs on `test/brew-bump-waiting-*` control branches, where reading the
        local file would report that branch's formula as "what users get"."""
        self.h.run()
        self.assertIn("contents/Formula/ironclaw.rb?ref=main", self.h.argv_log)

    # -----------------------------------------------------------------
    # Fail-loud: an unreadable API must never render as "the tap is current".
    # -----------------------------------------------------------------
    def test_unreadable_release_endpoint_is_red(self) -> None:
        self.h.force_failure("release.json")
        err = self.assertRed(self.h.run())
        self.assertIn("releases/latest", err)

    def test_unreadable_formula_is_red(self) -> None:
        self.h.force_failure("formula.rb")
        err = self.assertRed(self.h.run())
        self.assertIn("cannot tell what the tap is serving", err)

    def test_formula_with_no_version_line_is_red(self) -> None:
        """If the formula's shape changes, the parser must say so rather than compare an
        empty string and read as "not equal" or "equal" by accident."""
        self.h.set_formula_source("class Ironclaw < Formula\nend\n")
        err = self.assertRed(self.h.run())
        self.assertIn("no `  version", err)

    def test_draft_or_prerelease_latest_is_red(self) -> None:
        """releases/latest is documented to exclude both. If one ever appears, the
        endpoint's semantics changed and arm B would be comparing the tap against
        something users cannot install."""
        self.h.set_release("v0.1.450", published_minutes_ago=500, prerelease=True)
        err = self.assertRed(self.h.run())
        self.assertIn("prerelease=true", err)

    # -----------------------------------------------------------------
    # Arm A — preserved behaviour.
    # -----------------------------------------------------------------
    def test_waiting_pr_arm_still_fires_on_an_in_sync_tap(self) -> None:
        """Arm A catches a bump that IS trying to land, before the tap has trailed long
        enough for arm B. So it must fire with the formula in sync — and must not claim
        the tap is stale, which is the overclaim IRO-690 also fixed."""
        self.h.set_open_pr_age(700, minutes_ago=500)
        self.h.set_check_rollup(
            [
                {"name": "build", "conclusion": "SUCCESS"},
                {"name": "brew-formula-verify", "conclusion": "SUCCESS"},
                {"context": "CodeQL", "state": "SUCCESS"},
            ]
        )
        err = self.assertRed(self.h.run())
        self.assertIn("#700 open", err)
        self.assertIn("only the approving-review click is missing", err)
        self.assertNotIn("tap is STALE", err)

    def test_waiting_pr_under_threshold_with_in_sync_tap_is_green(self) -> None:
        self.h.set_open_pr_age(701, minutes_ago=5)
        out = self.assertGreen(self.h.run())
        self.assertIn("none past the 240m threshold", out)

    def test_missing_required_check_is_reported_as_the_blocker(self) -> None:
        """A required context that never REPORTS reads as not-green, so the alarm stays
        loud on exactly the PRs that are most stuck."""
        self.h.set_open_pr_age(702, minutes_ago=500)
        self.h.set_check_rollup([{"name": "build", "conclusion": "SUCCESS"}])
        err = self.assertRed(self.h.run())
        self.assertIn("required checks NOT green", err)
        self.assertIn("brew-formula-verify=MISSING", err)

    def test_both_arms_report_together(self) -> None:
        """One arm tripping must not hide the other: a stale tap AND a stuck bump PR are
        two different next actions and both belong in the notification. Arm B's
        remediation must also switch to its bump-PR-exists branch and drop the
        re-cut-the-bump instructions, which do not apply when a bump PR is sitting there."""
        self.h.set_open_pr_age(703, minutes_ago=500)
        self.h.set_check_rollup([{"name": "build", "conclusion": "FAILURE"}])
        self.h.set_formula_version(IRO689_FORMULA_VERSION)
        self.h.set_release(IRO689_LATEST_TAG, published_minutes_ago=500)
        err = self.assertRed(self.h.run())
        self.assertIn("tap is STALE", err)
        self.assertIn("#703 open", err)
        self.assertIn("A bump PR IS open", err)
        self.assertNotIn("ZERO open", err)
        self.assertNotIn("update-homebrew-formula.sh", err)

    def test_unreadable_pr_list_is_red(self) -> None:
        self.h.force_failure("pr_list.json")
        err = self.assertRed(self.h.run())
        self.assertIn("would make this guard unfalsifiable", err)

    def test_zero_threshold_is_rejected(self) -> None:
        err = self.assertRed(self.h.run(THRESHOLD_MINUTES="0"))
        self.assertIn("must be > 0", err)

    def test_non_numeric_stale_threshold_is_rejected(self) -> None:
        err = self.assertRed(self.h.run(STALE_THRESHOLD_MINUTES="4h"))
        self.assertIn("STALE_THRESHOLD_MINUTES must be a whole number", err)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
