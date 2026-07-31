#!/usr/bin/env python3
"""Fail the docs build when a markdown code fence is broken.

Nothing in CI reads markdown *structure*. PR #656 ("standardize code block
language identifiers") proved what that costs: it annotated 44 fences correctly
and, in ``docs/blog/audit-your-sandbox-in-10-seconds.md``, *deleted* the two
that wrapped the sample ``ironctl scan --md`` output instead of annotating them.
All 17 checks went green -- CodeQL, build, race, fuzz, wsg-verify,
sandbox-containment -- while the page silently started rendering the literal
sample text as a real ``###`` heading (which entered the page nav) and a real
table. A human reading the diff caught it. See IRO-707.

Three rules, and the second one is the interesting one:

``unclosed fence``
    An opening fence with no matching close before EOF. The textbook breakage,
    and the one every "fence guard" gets right. It is also the one that would
    *not* have caught #656: both fences were removed, so the file stayed
    perfectly balanced.

``stray blank line``
    Two or more consecutive blank lines outside a fenced block. This is the rule
    that catches a *deleted* fence. A fence line is conventionally preceded (if
    opening) or followed (if closing) by a blank line, so blanking one out --
    exactly what a botched fence edit does -- merges it into its neighbour and
    leaves a two-blank run where a fence used to be. On #656's head this fires
    on precisely the two damaged lines, 138 and 151, and nowhere else in the
    repo. It is deliberately a whitespace rule in service of a structural one:
    a deleted line leaves no other trace to assert on.

``bare opening fence``
    An opening fence with an empty info string. Untagged blocks lose syntax
    highlighting, and they are also what a careless "standardize the fences"
    sweep leaves behind, so the corpus staying tagged is what keeps the two
    rules above reading a consistent shape.

    This rule is live, on by default, with no suppression list -- but it could
    not ship with IRO-707. Immediately before #656 (``8c384e5``) the in-scope
    corpus carried 48 bare openers, so turning it on then would have meant
    either failing on main or shipping exactly the suppression list this rule
    now does without. #656 tagged 45 of them across 22 files; the last three
    (docs/assets/live-containment-social/STORYBOARD.md,
    docs/blog/scan-a-dockerfile-for-security-issues.md, docs/site/quickstart.mdx)
    were tagged in the commit that enabled this rule. See IRO-709 / IRO-710.

    Only *opening* fences are checked. A bare ``` inside a longer ```` block is
    block content, and the closing fence of any block is required to be bare --
    both fall out of the parser below rather than needing a special case.

Known limitation of the stray-blank-line rule: a fence indented inside a list
item is often written with no blank line around it, so deleting *that* kind of
fence leaves no double-blank and this check cannot see it. Requiring blank lines
around every fence would flag six legitimate list-embedded fences in
docs/providers/ollama.md and docs/pr-review-process.md, and a check that
pressures authors into breaking valid list markup is worse than a documented
gap.

``PATTERNS`` is deliberately narrower than the repo. 540 markdown files exist;
471 are in scope. The other 69 carry 33 bare openers of their own (20 under
examples/integrations, 9 under community/, plus api/, packaging/, runbooks/).
Widening the scope is a real proposal with a real diff attached, not a drive-by
edit to the tuple below -- the rules here are only as trustworthy as the
green they produce, and quietly growing the corpus is how a check starts
getting suppressed.

``CODE_OF_CONDUCT.md`` is excluded: it is a verbatim copy of the Contributor
Covenant, third-party licensed text we do not get to reformat, and it carries
seven double-blank runs of its own.

Usage:

    python3 scripts/check-md-fences.py [repo-root]

Exits 0 when every scanned file is clean, 1 on any finding, and 2 on a
usage/IO/vacuity error. Negative controls: scripts/tests/test_check_md_fences.py.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

# Every markdown surface a reader can reach. docs/site/**.mdx and
# docs/assets/**/*.md are excluded from the MkDocs build by `exclude_docs`, but
# a broken fence there is still a broken fence, so they are scanned too.
PATTERNS = ("docs/**/*.md", "docs/**/*.mdx", "*.md")

# Verbatim third-party licensed text; reformatting it is not ours to do.
EXCLUDED = frozenset({Path("CODE_OF_CONDUCT.md")})

# CommonMark: a fence is 3+ backticks or tildes, indented by at most 3 spaces.
FENCE = re.compile(r"^ {0,3}(?P<delim>`{3,}|~{3,})(?P<info>.*)$")


class Finding:
    """One rule violation, carrying the file:line it must be reported at."""

    def __init__(self, path: Path, line: int, message: str) -> None:
        self.path = path
        self.line = line
        self.message = message


def check_text(rel: Path, text: str) -> tuple[list[Finding], int]:
    """Apply all three rules to one file, returning findings and fences seen."""
    lines = text.split("\n")
    # split() on a trailing newline yields a phantom final "" that is EOF, not a
    # line. Dropping it keeps line numbers honest and stops every well-formed
    # file from reporting a trailing blank.
    if lines and lines[-1] == "":
        lines.pop()

    findings: list[Finding] = []
    fences = 0
    open_delim: str | None = None
    open_line = 0
    prev_blank = True  # start-of-file behaves like a blank for run detection

    for lineno, line in enumerate(lines, start=1):
        match = FENCE.match(line)
        if match:
            delim = match.group("delim")
            info = match.group("info").strip()
            if open_delim is None:
                # A backtick fence's info string may not contain a backtick, so
                # this is an inline code span (`` ```x`` ``), not a fence.
                if delim[0] == "`" and "`" in info:
                    prev_blank = False
                    continue
                open_delim, open_line = delim, lineno
                fences += 1
                prev_blank = False
                if not info:
                    findings.append(
                        Finding(
                            rel,
                            lineno,
                            "bare opening code fence: this block opens with no "
                            "language tag. Add one so the block is highlighted "
                            "and reads as deliberate -- ```text for literal "
                            "command output, ```bash for commands to run. The "
                            "closing fence stays bare",
                        )
                    )
                continue
            # A closing fence matches the opening delimiter, is at least as
            # long, and carries no info string. Anything else is block content.
            if delim[0] == open_delim[0] and len(delim) >= len(open_delim) and not info:
                open_delim = None
                prev_blank = False
                continue

        if open_delim is not None:
            # Inside a fenced block: blank lines are content (Go and Python both
            # use double blanks between declarations), so no rule applies.
            continue

        blank = line.strip() == ""
        if blank and prev_blank and lineno > 1:
            findings.append(
                Finding(
                    rel,
                    lineno,
                    "stray blank line: two consecutive blank lines outside a code "
                    "block. A deleted fence leaves exactly this signature -- check "
                    "whether a ``` opening or closing this block was removed",
                )
            )
        prev_blank = blank

    if open_delim is not None:
        findings.append(
            Finding(
                rel,
                open_line,
                f"unclosed code fence: `{open_delim}` opens a block that never "
                "closes before end of file. If this block's closing fence was "
                "given a language tag, remove the tag -- a closing fence takes no "
                "info string and is read as a new opening fence",
            )
        )

    return findings, fences


def collect(root: Path) -> list[Path]:
    """Every in-scope markdown path, relative to the repo root, deduplicated."""
    seen: set[Path] = set()
    for pattern in PATTERNS:
        for path in root.glob(pattern):
            rel = path.relative_to(root)
            if rel not in EXCLUDED and path.is_file():
                seen.add(rel)
    return sorted(seen)


def main(argv: list[str]) -> int:
    if len(argv) > 2:
        print(f"usage: {argv[0]} [repo-root]", file=sys.stderr)
        return 2
    root = Path(argv[1]) if len(argv) == 2 else Path.cwd()

    if not (root / "docs").is_dir():
        print(f"::error::{root / 'docs'} is not a directory", file=sys.stderr)
        return 2

    paths = collect(root)
    # A glob that matched nothing would report a green "all fences balanced"
    # over an empty set. Refuse to be that check.
    if not paths:
        print(
            f"::error::no markdown matched {' '.join(PATTERNS)} under {root}; "
            "this check would pass vacuously",
            file=sys.stderr,
        )
        return 2

    findings: list[Finding] = []
    fences = 0
    for rel in paths:
        try:
            text = (root / rel).read_text(encoding="utf-8")
        except OSError as exc:
            print(f"::error::cannot read {rel}: {exc}", file=sys.stderr)
            return 2
        file_findings, file_fences = check_text(rel, text)
        findings.extend(file_findings)
        fences += file_fences

    if findings:
        for finding in findings:
            print(
                f"::error file={finding.path},line={finding.line}::"
                f"{finding.path}:{finding.line}: {finding.message}",
                file=sys.stderr,
            )
        print(
            f"\n{len(findings)} finding(s) across {len(paths)} markdown file(s).",
            file=sys.stderr,
        )
        return 1

    # Name what was actually enumerated: "no failures" and "nothing was checked"
    # have to be distinguishable in the log.
    print(
        f"ok: {len(paths)} markdown files, {fences} fenced blocks, "
        "every block opened with a language tag, closed, and no stray blank lines"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
