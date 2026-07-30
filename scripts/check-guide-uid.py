#!/usr/bin/env python3
"""Fail the docs build when a hardening guide misquotes the `--fix` uid.

Every ``docs/blog/harden-*.md`` opens its remediation section with the words
"Harden it: the exact ``--fix`` remediation". That heading is a claim about our
own tool's output, so the flags under it have to *be* that output. Twice now
they were not, and both times a human reading the page caught it rather than
CI: IRO-698 shipped a guide whose bullet and run block pinned different uids,
and the IRO-699 sweep that followed found 17 guides quoting a uid ``--fix``
never emits, two of them (couchdb ``5984``, neo4j ``7474``) reusing the image's
own port number.

Nothing else in CI can see this. ``check_links.py`` and ``mkdocs build
--strict`` only follow links; ``check-guide-index.py`` only checks nav wiring;
``check-docs-meta.py`` only reads front-matter. A wrong-but-well-formed number
in a code fence is invisible to all three.

Two properties are asserted, and the split matters:

1. **Attribution.** ``hardenedUID`` in ``internal/host/scan/remediate.go`` is a
   compile-time constant with no image input, so ``--fix`` emits the same uid
   for every image. In a guide whose *own* default-scan table reports FAIL on
   the non-root dimension, ``--fix`` genuinely remediates that dimension, so
   every ``--user`` in the page must be that constant.

   The uid is read out of the Go source, never hardcoded here, so changing the
   constant moves the assertion with it instead of turning this check into a
   second thing to remember.

2. **Internal consistency.** A pasted "after" scan block is the output of the
   hardened ``docker run`` above it, so its ``runs as`` detail has to match
   what that block actually sets: the block's ``--user`` when it has one, and
   otherwise the user from the guide's own default-scan table, because a run
   with no ``--user`` does not change who the image runs as. This is the check
   that catches a bullet and a run block disagreeing, and it is what resolved
   pgadmin4, whose hardened block sets no ``--user`` at all yet claimed an
   "after" uid that nothing in the guide set.

Guides whose default table already PASSes non-root are deliberately exempt from
(1). There ``--fix`` emits no user remediation, and the guide is pinning the
image's *own* uid: haproxy ``99:99``, grafana ``472:472``, prometheus
``nobody``. Rewriting those to 65532 would replace a true statement with a
false one and break the container besides, since the data volume is owned by
the image's uid. They stay under (2).

Also out of scope: ``docs/scores/**``. ``1000:1000`` there is real scan output
for an image that really does run as uid 1000, not a ``--fix`` recommendation.
This check never reads that tree.

Usage:

    python3 scripts/check-guide-uid.py [repo-root]

Exits 0 when every guide agrees with the Go constant and with itself, 1 on any
mismatch, and 2 on a usage/IO error or on anything that would make the run
vacuous. Negative controls: scripts/tests/test_check_guide_uid.py.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

BLOG_DIR = Path("docs/blog")
GUIDE_GLOB = "harden-*.md"

# The single source of truth for the uid. Read, never hardcoded.
REMEDIATE_GO = Path("internal/host/scan/remediate.go")
HARDENED_UID_RE = re.compile(r'^const\s+hardenedUID\s*=\s*"([^"]+)"', re.MULTILINE)

# `| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as pgadmin (uid != 0) |`
TABLE_ROW_RE = re.compile(
    r"^\|\s*Non-root user \(uid != 0\)\s*\|[^|]*?\b(PASS|FAIL|WARN)\b[^|]*\|"
    r"[^|]*\|\s*(.*?)\s*\|\s*$",
    re.MULTILINE,
)

# `Non-root user (uid != 0)    [+] PASS  15/15  runs as 65532:65532 (uid != 0)`
# inside a pasted scan-output fence.
OUTPUT_ROW_RE = re.compile(
    r"^Non-root user \(uid != 0\)\s+\[.\]\s+(PASS|FAIL|WARN)\s+\S+\s+(.*?)\s*$",
    re.MULTILINE,
)

# `runs as <user> (uid != 0)` -- the scorer's PASS detail (score.go).
RUNS_AS_RE = re.compile(r"runs as (\S+) \(uid != 0\)")

# A `--user` flag, in a bullet (`--user 65532:65532`) or a run block.
USER_FLAG_RE = re.compile(r"--user\s+`?([^\s`\\]+)")

# The hardened `docker run`: the fenced block introduced by a `# After:` comment.
AFTER_BLOCK_RE = re.compile(r"^# After:.*?(?=^```|\Z)", re.MULTILINE | re.DOTALL)


def read_hardened_uid(root: Path) -> str:
    path = root / REMEDIATE_GO
    text = path.read_text(encoding="utf-8")
    match = HARDENED_UID_RE.search(text)
    if match is None:
        # The constant moving or being renamed must break loudly. Silently
        # skipping the assertion would leave a green check over an unchecked
        # docs tree, which is the failure mode this whole file exists to stop.
        raise LookupError(f"no `const hardenedUID = \"...\"` in {REMEDIATE_GO}")
    return match.group(1)


def check_guide(path: Path, text: str, hardened_uid: str) -> tuple[list[str], int, int]:
    """Return (failures, user_flags_asserted, output_rows_asserted)."""
    failures: list[str] = []
    rel = BLOG_DIR / path.name

    table = TABLE_ROW_RE.search(text)
    if table is None:
        failures.append(
            f"::error file={rel}::no `| Non-root user (uid != 0) | ... |` row in the "
            "default-scan table, so this guide's uid claims cannot be checked; "
            "keep the table row or this guide ships unguarded"
        )
        return failures, 0, 0
    default_verdict, default_detail = table.group(1), table.group(2)

    user_flags = [
        (m.group(1), text.count("\n", 0, m.start()) + 1)
        for m in USER_FLAG_RE.finditer(text)
    ]

    asserted_flags = 0
    if default_verdict == "FAIL":
        # `--fix` remediates the non-root dimension here, so every uid on the
        # page is a claim about what it emits.
        for value, line in user_flags:
            asserted_flags += 1
            if value != hardened_uid:
                failures.append(
                    f"::error file={rel},line={line}::`--user {value}` is attributed to "
                    f"`ironctl scan --fix`, but --fix emits `--user {hardened_uid}` "
                    f"(hardenedUID in {REMEDIATE_GO}). This guide's own scan table "
                    "reports FAIL on the non-root dimension, so --fix does remediate "
                    f"it; use {hardened_uid} or correct the table."
                )

    # What the hardened run block actually sets. No `--user` means the container
    # keeps the image's own user, which the default table already names.
    after = AFTER_BLOCK_RE.search(text)
    after_user = None
    if after is not None:
        flag = USER_FLAG_RE.search(after.group(0))
        if flag is not None:
            after_user = flag.group(1)

    expected = after_user
    if expected is None:
        seen = RUNS_AS_RE.search(default_detail)
        expected = seen.group(1) if seen is not None else None

    asserted_rows = 0
    for match in OUTPUT_ROW_RE.finditer(text):
        detail = match.group(2)
        seen = RUNS_AS_RE.search(detail)
        if seen is None:
            continue
        line = text.count("\n", 0, match.start()) + 1
        if expected is None:
            failures.append(
                f"::error file={rel},line={line}::pasted scan output claims "
                f"`runs as {seen.group(1)}`, but neither the hardened run block nor "
                "the default-scan table names a user to compare it against"
            )
            continue
        asserted_rows += 1
        if seen.group(1) != expected:
            source = (
                f"the hardened run block sets `--user {after_user}`"
                if after_user is not None
                else f"the hardened run block sets no `--user`, and the default-scan "
                f"table reports `runs as {expected}`"
            )
            failures.append(
                f"::error file={rel},line={line}::pasted scan output claims "
                f"`runs as {seen.group(1)}`, but {source}. A reader copying the block "
                "above does not get this output; make them agree."
            )

    return failures, asserted_flags, asserted_rows


def main(argv: list[str]) -> int:
    if len(argv) > 2:
        print(f"usage: {argv[0]} [repo-root]", file=sys.stderr)
        return 2
    root = Path(argv[1]) if len(argv) == 2 else Path.cwd()

    blog = root / BLOG_DIR
    if not blog.is_dir():
        print(f"::error::{blog} is not a directory", file=sys.stderr)
        return 2

    try:
        hardened_uid = read_hardened_uid(root)
    except (OSError, LookupError) as exc:
        print(f"::error::cannot read the hardened uid: {exc}", file=sys.stderr)
        return 2

    guides = sorted(blog.glob(GUIDE_GLOB))
    if not guides:
        print(
            f"::error::no {GUIDE_GLOB} guides under {blog}; this check would pass "
            "vacuously",
            file=sys.stderr,
        )
        return 2

    failures: list[str] = []
    total_flags = 0
    total_rows = 0
    for path in guides:
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as exc:
            print(f"::error::cannot read {path}: {exc}", file=sys.stderr)
            return 2
        found, flags, rows = check_guide(path, text, hardened_uid)
        failures.extend(found)
        total_flags += flags
        total_rows += rows

    # A green run has to mean "the assertions ran and held", not "there was
    # nothing to assert". Every count below was non-zero when this check was
    # written; a zero means the guides' shape drifted out from under the
    # parser and the check is no longer looking at anything.
    if not failures:
        if total_flags == 0:
            print(
                "::error::matched 0 `--user` flags in any guide whose non-root "
                "dimension FAILs by default; the parser has drifted and this check "
                "is asserting nothing",
                file=sys.stderr,
            )
            return 2
        if total_rows == 0:
            print(
                "::error::matched 0 pasted scan-output rows to compare against a "
                "hardened run block; the parser has drifted and this check is "
                "asserting nothing",
                file=sys.stderr,
            )
            return 2

    if failures:
        for failure in failures:
            print(failure, file=sys.stderr)
        print(
            f"\n{len(failures)} uid mismatch(es) across {len(guides)} guide(s).",
            file=sys.stderr,
        )
        return 1

    # Name what was actually enumerated: "no failures" and "nothing was checked"
    # have to be distinguishable in the log.
    print(
        f"ok: {len(guides)} hardening guides; {total_flags} `--user` flag(s) match "
        f"hardenedUID={hardened_uid} from {REMEDIATE_GO}; {total_rows} pasted "
        "scan-output row(s) match their hardened run block"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
