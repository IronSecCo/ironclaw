#!/usr/bin/env python3
"""Fail the docs build when committed ``docs/scores/`` drifts from its generator.

Every page under ``docs/scores/`` -- 252 scorecards plus index.md,
leaderboard.md, index.json and the collection pages -- is *emitted* by
``examples/isolation-survey/gen_scorecards.py`` from
``examples/isolation-survey/results.json``. The tree is committed so the docs
site builds without re-running the survey, which means the committed bytes and
the generator can disagree and nothing notices.

Two ways that hurts, both of them quiet:

* A hand-edit of a generated page looks like a normal docs change, passes every
  other check here, and is silently reverted by the next refresh.
* ``.github/workflows/scores-refresh.yml`` re-runs the generator weekly and
  force-opens a rolling PR. A generator change that landed without re-emitting
  is picked up there and rewritten across all 252 published pages, inside a diff
  that is already full of legitimate score movement -- a broken template is at
  its least visible exactly where it does the most damage (IRO-715).

So this regenerates into a scratch directory and compares byte-for-byte against
what is committed. The generator is deterministic (pages keyed by image slug,
rows sorted by ``(score, slug)``), so a clean tree compares equal every time.

Two deliberate carve-outs:

* ``docs/scores/.nav.yml`` and ``docs/scores/collections/.nav.yml`` are
  hand-maintained -- the generator never emits them -- so any file named
  ``.nav.yml`` is excluded. A naive ``diff -r`` reports them and fails.
* The generator prints ``warning: no family mapping for slug ...`` to stderr on
  a perfectly clean run. Only its exit status is read; stderr is not failure.

Usage:

    python3 scripts/check-scores-drift.py [repo-root]

Exits 0 when every generated file matches, 1 on drift, and 2 on a usage/IO error
or a generator that will not run. Negative controls:
scripts/tests/test_check_scores_drift.py.
"""

from __future__ import annotations

import hashlib
import subprocess
import sys
import tempfile
from pathlib import Path

GENERATOR = Path("examples/isolation-survey/gen_scorecards.py")
RESULTS = Path("examples/isolation-survey/results.json")
SCORES_DIR = Path("docs/scores")

# Hand-maintained, never emitted by the generator. Matched on basename, which
# covers both docs/scores/.nav.yml and docs/scores/collections/.nav.yml.
EXCLUDED_NAMES = {".nav.yml"}


def generated_files(root: Path) -> dict[str, str]:
    """Map repo-relative-to-`root` path -> sha256, skipping the excluded names."""
    digests = {}
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.name in EXCLUDED_NAMES:
            continue
        digests[str(path.relative_to(root))] = hashlib.sha256(
            path.read_bytes()
        ).hexdigest()
    return digests


def main(argv: list[str]) -> int:
    if len(argv) > 2:
        print(f"usage: {argv[0]} [repo-root]", file=sys.stderr)
        return 2
    root = Path(argv[1]) if len(argv) == 2 else Path(__file__).resolve().parent.parent

    generator, results, committed_dir = root / GENERATOR, root / RESULTS, root / SCORES_DIR
    for label, path in (("generator", generator), ("results", results)):
        if not path.is_file():
            print(f"::error::{label} not found at {path}", file=sys.stderr)
            return 2
    if not committed_dir.is_dir():
        print(f"::error::no committed scores tree at {committed_dir}", file=sys.stderr)
        return 2

    with tempfile.TemporaryDirectory() as tmp:
        out_dir = Path(tmp) / "scores"
        proc = subprocess.run(
            [sys.executable, str(generator), str(results), str(out_dir)],
            capture_output=True,
            text=True,
        )
        if proc.returncode != 0:
            # A generator that cannot run is a failure of this check, not a pass.
            print(
                f"::error file={GENERATOR}::generator exited {proc.returncode}; "
                f"docs/scores/ cannot be re-derived",
                file=sys.stderr,
            )
            print(proc.stdout + proc.stderr, file=sys.stderr)
            return 2

        fresh = generated_files(out_dir)
        committed = generated_files(committed_dir)

    # "Nothing differs" and "nothing was compared" must not look alike.
    if not fresh:
        print(
            f"::error file={GENERATOR}::generator emitted no files; nothing was compared",
            file=sys.stderr,
        )
        return 2

    missing = sorted(set(fresh) - set(committed))
    extra = sorted(set(committed) - set(fresh))
    changed = sorted(
        rel for rel in set(fresh) & set(committed) if fresh[rel] != committed[rel]
    )

    for rel in missing:
        print(
            f"::error file={SCORES_DIR / rel}::the generator emits this file but it "
            f"is not committed; re-run the generator and commit the result",
            file=sys.stderr,
        )
    for rel in extra:
        print(
            f"::error file={SCORES_DIR / rel}::committed but the generator does not "
            f"emit it; it is either stale or a hand-edit that the next "
            f"scores-refresh run will delete",
            file=sys.stderr,
        )
    for rel in changed:
        print(
            f"::error file={SCORES_DIR / rel}::committed bytes differ from what the "
            f"generator emits; re-run the generator and commit the result",
            file=sys.stderr,
        )

    if missing or extra or changed:
        print(
            f"\n{len(missing) + len(extra) + len(changed)} drifted file(s) "
            f"({len(missing)} uncommitted, {len(extra)} stale, {len(changed)} "
            f"modified) out of {len(fresh)} generated. Regenerate with:\n"
            f"  python3 {GENERATOR} {RESULTS} {SCORES_DIR}",
            file=sys.stderr,
        )
        return 1

    # Name the count that was actually compared: a green run has to be
    # distinguishable from a run that enumerated nothing.
    print(
        f"ok: {len(fresh)} generated files under {SCORES_DIR} are byte-identical to "
        f"{GENERATOR} output (excluding hand-maintained "
        f"{', '.join(sorted(EXCLUDED_NAMES))})"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
