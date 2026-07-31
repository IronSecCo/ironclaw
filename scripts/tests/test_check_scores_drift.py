#!/usr/bin/env python3
"""Tests for scripts/check-scores-drift.py (IRO-715).

This checker's passing output is "exit 0", which is also what a checker that
compared nothing produces -- and on a clean tree it is green by construction, so
a green run proves very little on its own. Every test below is therefore a
negative control: each builds a tree that IS drifted (or degenerate) and asserts
the checker refuses to call it green.

Two of them pin the specific traps called out on IRO-715, both of which a naive
`diff -r docs/scores <regen>` gets wrong:

* test_hand_maintained_nav_is_not_drift -- `.nav.yml` is hand-maintained and the
  generator never emits it, so a plain `diff -r` reports it and fails on a
  perfectly clean tree.
* test_generator_stderr_is_not_failure -- the real generator prints `warning: no
  family mapping for slug ...` to stderr on a clean run; treating stderr as
  failure would make this check red on `main` from day one.

test_real_repo_is_clean is the non-vacuity anchor in the other direction: it runs
against the actual committed tree, so if the fake-tree tests ever passed for the
wrong reason (a stub that does not resemble the generator), this one still holds
the checker to real bytes.

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

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / 'check-scores-drift.py'
REPO_ROOT = SCRIPT.parent.parent

# A stub standing in for gen_scorecards.py: same contract (argv = results.json,
# out_dir), writing a deterministic two-file tree. Keeps the tests to
# milliseconds instead of re-emitting 252 real pages per case.
STUB_GENERATOR = '''\
import json, os, sys
results, out_dir = sys.argv[1], sys.argv[2]
data = json.load(open(results))
os.makedirs(os.path.join(out_dir, "collections"), exist_ok=True)
for name, body in data["pages"].items():
    with open(os.path.join(out_dir, name), "w") as f:
        f.write(body)
'''


def load_module():
    spec = importlib.util.spec_from_file_location('check_scores_drift', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root: pathlib.Path):
    """Run the checker against `root`, returning (exit code, stdout+stderr)."""
    module = load_module()
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = module.main(['check-scores-drift.py', str(root)])
    return code, out.getvalue() + err.getvalue()


def build_tree(root: pathlib.Path, pages, committed=None, generator=STUB_GENERATOR):
    """Write a fake repo: a stub generator, its results.json, and docs/scores/.

    `pages` is what the generator will emit; `committed` is what lands in
    docs/scores/ (defaults to `pages`, i.e. a clean tree).
    """
    survey = root / 'examples' / 'isolation-survey'
    survey.mkdir(parents=True, exist_ok=True)
    (survey / 'gen_scorecards.py').write_text(generator, encoding='utf-8')
    (survey / 'results.json').write_text(
        __import__('json').dumps({'pages': pages}), encoding='utf-8'
    )
    scores = root / 'docs' / 'scores'
    (scores / 'collections').mkdir(parents=True, exist_ok=True)
    for name, body in (pages if committed is None else committed).items():
        (scores / name).write_text(body, encoding='utf-8')
    return root


@contextlib.contextmanager
def temp_root():
    with tempfile.TemporaryDirectory() as tmp:
        yield pathlib.Path(tmp)


class CheckScoresDriftTest(unittest.TestCase):
    def test_clean_tree_passes(self):
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'score 41\n', 'index.md': 'directory\n'})
            code, output = run(root)
            self.assertEqual(code, 0, output)
            # The green line must name what it compared, so "nothing differed"
            # and "nothing was compared" stay distinguishable in the log.
            self.assertIn('2 generated files', output)

    def test_hand_edited_page_is_caught(self):
        """The quiet case: a generated page edited by hand, reverted next refresh."""
        with temp_root() as root:
            pages = {'nginx.md': 'score 41\n', 'index.md': 'directory\n'}
            build_tree(root, pages, committed={**pages, 'nginx.md': 'score 99\n'})
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('nginx.md', output)
            self.assertIn('differ from what the generator emits', output)

    def test_generator_change_without_regenerating_is_caught(self):
        """IRO-715's actual scenario: template moves, docs/scores/ does not."""
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'score 41\n'})
            # Same shape as editing the generator's template: emitted bytes move
            # while the committed tree stays where it was.
            (root / 'examples' / 'isolation-survey' / 'gen_scorecards.py').write_text(
                STUB_GENERATOR.replace('f.write(body)', 'f.write("NEW TEMPLATE\\n" + body)'),
                encoding='utf-8',
            )
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('nginx.md', output)

    def test_stale_committed_page_is_caught(self):
        with temp_root() as root:
            pages = {'nginx.md': 'score 41\n'}
            build_tree(root, pages, committed={**pages, 'dropped.md': 'gone\n'})
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('dropped.md', output)
            self.assertIn('generator does not', output)

    def test_uncommitted_generated_page_is_caught(self):
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'score 41\n', 'redis.md': 'score 20\n'},
                       committed={'nginx.md': 'score 41\n'})
            code, output = run(root)
            self.assertEqual(code, 1, output)
            self.assertIn('redis.md', output)
            self.assertIn('not committed', output)

    def test_hand_maintained_nav_is_not_drift(self):
        """`.nav.yml` is hand-maintained; a plain `diff -r` fails a clean tree on it."""
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'score 41\n'})
            scores = root / 'docs' / 'scores'
            (scores / '.nav.yml').write_text('nav:\n  - index.md\n', encoding='utf-8')
            (scores / 'collections' / '.nav.yml').write_text('nav:\n', encoding='utf-8')
            code, output = run(root)
            self.assertEqual(code, 0, output)

    def test_generator_stderr_is_not_failure(self):
        """The real generator warns on stderr on every clean run."""
        with temp_root() as root:
            build_tree(
                root,
                {'nginx.md': 'score 41\n'},
                generator=STUB_GENERATOR
                + 'sys.stderr.write("warning: no family mapping for slug \'x\'\\n")\n',
            )
            code, output = run(root)
            self.assertEqual(code, 0, output)

    def test_broken_generator_is_not_green(self):
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'x\n'}, generator='import sys; sys.exit(3)\n')
            code, output = run(root)
            self.assertEqual(code, 2, output)
            self.assertIn('cannot be re-derived', output)

    def test_generator_emitting_nothing_is_not_green(self):
        """An empty emission compares equal to nothing; that must not read as pass."""
        with temp_root() as root:
            build_tree(root, {}, committed={})
            code, output = run(root)
            self.assertEqual(code, 2, output)
            self.assertIn('nothing was compared', output)

    def test_missing_generator_is_not_green(self):
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'x\n'})
            (root / 'examples' / 'isolation-survey' / 'gen_scorecards.py').unlink()
            code, output = run(root)
            self.assertEqual(code, 2, output)
            self.assertIn('generator not found', output)

    def test_missing_scores_dir_is_not_green(self):
        with temp_root() as root:
            build_tree(root, {'nginx.md': 'x\n'})
            (root / 'docs' / 'scores' / 'nginx.md').unlink()
            (root / 'docs' / 'scores' / 'collections').rmdir()
            (root / 'docs' / 'scores').rmdir()
            code, output = run(root)
            self.assertEqual(code, 2, output)
            self.assertIn('no committed scores tree', output)

    def test_real_repo_is_clean(self):
        """Non-vacuity anchor: the real committed tree, real generator, real bytes."""
        code, output = run(REPO_ROOT)
        self.assertEqual(code, 0, output)
        self.assertIn('byte-identical', output)


if __name__ == '__main__':
    unittest.main()
