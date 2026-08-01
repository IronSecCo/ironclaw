#!/usr/bin/env python3
"""Fail the docs build when a hardening guide's score table does not add up.

Every ``docs/blog/harden-*-container-isolation.md`` opens with a seven-row
dimension table and then restates its total in four places: the front-matter
``title:``, the front-matter ``description:``, the body prose, and a
``# Before: NN/100`` comment in the run block. Nothing in the pipeline reads any
of it. ``check-scores-drift.py`` is the closest thing, and its ``SCORES_DIR`` is
``docs/scores``, so ``docs/blog/**`` is outside every existing guard.

What that costs, concretely: PR #661 rewrote the HAProxy guide so its table
summed to 58 while the page claimed 63/100 in four places, invented a
``Privilege escalation`` dimension the scorer does not have, deleted the
canonical ``No docker.sock exposure`` row, and moved ``Read-only root
filesystem`` from its real ``0/10`` to ``5/15``. All 17 checks went green --
``build``, ``CodeQL``, ``brew-formula-verify``, ``check-guide-uid.py``,
``check-guide-index.py``, ``check-md-fences.py``. The camouflage is that the
*denominators* still totalled 100 (rootfs +5, docker.sock 15 -> privesc 10), so
only the scores column was short, by exactly 5. It took four rounds of manual
review to catch. This is that review, mechanised.

Five checks, in order of what they catch:

1. **Arithmetic.** The scores column sums to the number the page states, at
   every site that states it. A page claiming its score in four places and
   summing to a fifth number fails.
2. **Dimension integrity.** Row titles and ``max`` weights must equal the
   canonical ordered set, so an invented row, a deleted row, a reordered row or
   a reweighted row all fail.
3. **Verdict reachability.** A row's ``(score, verdict)`` pair must be one the
   scorer can actually emit. ``gradeReadonly`` returns only ``10/PASS`` or
   ``0/FAIL``, so #661's ``5/15 WARN`` rootfs row is unreachable and fails even
   though 5 is a perfectly plausible-looking number.
4. **Grade band.** The letter stated next to a total must be the band
   ``grade()`` puts that total in.
5. **Agreement with measured data.** Where the guide's image has a ``default-*``
   scenario in ``examples/isolation-survey/results.json``, every row's score,
   max and verdict must equal what was actually recorded, and so must the total,
   the grade and the failed-dimension set.

Everything canonical is derived from source, never copied:

* the dimension set, titles and weights come from the ``scorers`` slice in
  ``internal/host/scan/score.go``;
* the reachable ``(score, verdict)`` pairs come from parsing the ``grade*``
  function bodies in the same file;
* the letter bands come from its ``grade()`` switch;
* the measured values come from ``results.json``.

A copy would rot the moment a dimension is added, and rot silently -- which is
the exact failure mode this guard exists to close. When the parser cannot find
what it expects in ``score.go`` it exits 2 with the reason rather than passing
with an empty canonical set; a guard that is green because it parsed nothing is
worse than no guard.

``gen_scorecards.py`` documents the same seven dimensions in prose for readers.
That copy is cross-checked against ``score.go`` and a divergence is reported as
``canonical-mirror``: it would mean the scorer and the published methodology
disagree, which is a product bug and not something to paper over here.

**Scope: the "Before" table only.** 35 guides plus the flagship comparison page
publish post-hardening ("After") numbers that were never measured; that is
IRO-726 and it is a wording call owned there. This guard reads the default-
configuration table and the sites that restate its total. "After" totals
(``take the same image to **89 of 100**``, ``# After: 89/100``, ``Rescan: ...
reports 89/100``) are deliberately not validated.

Usage:

    python3 scripts/check-guide-scores.py [--report] [--min-guides N] [root]

    --report        print the violations and exit 0 (rollout mode)
    --min-guides N  fail if fewer than N guides were parsed (default 56)

Exits 0 when every guide is consistent, 1 on violations, and 2 on a usage/IO
error or a ``score.go`` this parser no longer understands. Negative controls:
scripts/tests/test_check_guide_scores.py.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

SCORE_GO = Path("internal/host/scan/score.go")
GEN_SCORECARDS = Path("examples/isolation-survey/gen_scorecards.py")
RESULTS = Path("examples/isolation-survey/results.json")
GUIDE_GLOB = "docs/blog/harden-*-container-isolation.md"

# The corpus is 56 guides today. Asserted, not assumed: this whole script is a
# response to a check that was green for the wrong reason, so it refuses to be
# green because it found nothing to read.
DEFAULT_MIN_GUIDES = 56


class ParseError(Exception):
    """Source no longer matches what this parser expects -- exit 2, not 0."""


# --------------------------------------------------------------------------- #
# Canonical values, parsed out of internal/host/scan/score.go.
# --------------------------------------------------------------------------- #

# {"user.nonroot", "Non-root user (uid != 0)", 15, gradeNonRoot},
_SCORER_RE = re.compile(
    r'\{"(?P<key>[^"]+)",\s*"(?P<title>[^"]+)",\s*(?P<max>\d+),\s*(?P<fn>\w+)\}'
)
_SCORERS_BLOCK_RE = re.compile(r"var scorers = \[\]scorer\{(.*?)\n\}", re.S)
_TOTAL_WEIGHT_RE = re.compile(r"const TotalWeight = (\d+)")


class Dimension:
    def __init__(self, key: str, title: str, max_: int, fn: str) -> None:
        self.key = key
        self.title = title
        self.max = max_
        self.fn = fn


def parse_scorers(score_go: str) -> list[Dimension]:
    """The ordered dimension set. Order is meaningful: the tables mirror it."""
    block = _SCORERS_BLOCK_RE.search(score_go)
    if not block:
        raise ParseError(
            "could not find `var scorers = []scorer{...}` in %s; the canonical "
            "dimension set moved and this parser needs updating" % SCORE_GO
        )
    dims = [
        Dimension(m["key"], m["title"], int(m["max"]), m["fn"])
        for m in _SCORER_RE.finditer(block.group(1))
    ]
    if not dims:
        raise ParseError("parsed zero dimensions from the `scorers` slice in %s" % SCORE_GO)

    total = _TOTAL_WEIGHT_RE.search(score_go)
    if not total:
        raise ParseError("could not find `const TotalWeight` in %s" % SCORE_GO)
    want = int(total.group(1))
    got = sum(d.max for d in dims)
    if got != want:
        raise ParseError(
            "dimension weights in %s sum to %d but TotalWeight is %d" % (SCORE_GO, got, want)
        )
    return dims


# case score >= 90: return "A"
_BAND_RE = re.compile(r"case score >= (\d+):\s*\n\s*return \"([A-F])\"")
_GRADE_FUNC_RE = re.compile(r"^func grade\(score int\) string \{(.*?)^\}", re.S | re.M)
_GRADE_DEFAULT_RE = re.compile(r"default:\s*\n\s*return \"([A-F])\"")


def parse_bands(score_go: str) -> list[tuple[int, str]]:
    """[(90, 'A'), (75, 'B'), ...] plus a (0, 'F') floor, from grade()."""
    body = _GRADE_FUNC_RE.search(score_go)
    if not body:
        raise ParseError("could not find `func grade(score int) string` in %s" % SCORE_GO)
    bands = [(int(lo), letter) for lo, letter in _BAND_RE.findall(body.group(1))]
    if not bands:
        raise ParseError("parsed zero letter bands from grade() in %s" % SCORE_GO)
    fallthrough = _GRADE_DEFAULT_RE.search(body.group(1))
    if not fallthrough:
        raise ParseError("grade() in %s has no default branch" % SCORE_GO)
    bands.sort(key=lambda b: -b[0])
    bands.append((0, fallthrough.group(1)))
    return bands


def grade_for(score: int, bands: list[tuple[int, str]]) -> str:
    for low, letter in bands:
        if score >= low:
            return letter
    raise ParseError("no band matched score %d" % score)


# return 15, VerdictPass, ...
_LITERAL_RETURN_RE = re.compile(r"return (\d+), Verdict(\w+)")
# return pts, VerdictWarn, ...
_VAR_RETURN_RE = re.compile(r"return ([a-z]\w*), Verdict(\w+)")
# pts := 20 - 4*len(s.CapAdd)  /  if pts < 6 { pts = 6 }
_SCALED_RE = re.compile(r"(\w+) := (\d+) - (\d+)\*len\([^)]*\)")
_FLOOR_RE = re.compile(r"if (\w+) < (\d+) \{\s*\n\s*\1 = \2\s*\n\s*\}")


def parse_reachable(score_go: str, dims: list[Dimension]) -> dict[str, set[tuple[int, str]]]:
    """Map dimension key -> the (score, VERDICT) pairs its scorer can emit.

    Read out of the Go, not restated here. Most branches are literal
    ``return <int>, Verdict<X>``. ``gradeCaps`` has one computed branch --
    ``pts := 20 - 4*len(s.CapAdd)`` floored at 6, for capabilities added back
    after ``--cap-drop=ALL`` -- which is expanded from the arithmetic. Any other
    non-literal return is a shape this parser does not understand, and it says
    so (exit 2) rather than quietly accepting every value for that dimension.
    """
    reachable: dict[str, set[tuple[int, str]]] = {}
    for dim in dims:
        body_re = re.compile(r"^func %s\(s Spec\) \(int, Verdict, string\) \{(.*?)^\}" % dim.fn,
                             re.S | re.M)
        match = body_re.search(score_go)
        if not match:
            raise ParseError("could not find `func %s` in %s" % (dim.fn, SCORE_GO))
        body = match.group(1)

        pairs = {(int(n), v.upper()) for n, v in _LITERAL_RETURN_RE.findall(body)}

        for var, verdict in _VAR_RETURN_RE.findall(body):
            scaled = _SCALED_RE.search(body)
            floor = _FLOOR_RE.search(body)
            if not (scaled and floor and scaled.group(1) == var == floor.group(1)):
                raise ParseError(
                    "func %s returns non-literal `%s` and this parser cannot derive its "
                    "range; update parse_reachable() rather than letting the dimension "
                    "accept any score" % (dim.fn, var)
                )
            base, step, lowest = int(scaled.group(2)), int(scaled.group(3)), int(floor.group(2))
            value = base - step
            while value > lowest:
                pairs.add((value, verdict.upper()))
                value -= step
            pairs.add((lowest, verdict.upper()))

        if not pairs:
            raise ParseError("parsed zero reachable outcomes from func %s" % dim.fn)
        # Fail-closed: a dimension can always be UNKNOWN at 0 in image mode, but
        # a guide table never renders that, so it is not added here.
        reachable[dim.key] = pairs
    return reachable


# - **Non-root user** (15 pts) — does it drop host uid 0?
_MIRROR_RE = re.compile(r'"- \*\*(?P<title>[^*]+)\*\* \((?P<max>\d+) pts\)')


def check_mirror(gen_src: str, dims: list[Dimension]) -> list[str]:
    """The published methodology bullets vs score.go. A divergence is a product bug."""
    mirrored = [(m["title"].strip(), int(m["max"])) for m in _MIRROR_RE.finditer(gen_src)]
    if not mirrored:
        return [
            "canonical-mirror: parsed zero dimension bullets from %s; the mirror moved "
            "and can no longer be cross-checked against %s" % (GEN_SCORECARDS, SCORE_GO)
        ]
    problems = []
    if len(mirrored) != len(dims):
        problems.append(
            "canonical-mirror: %s documents %d dimensions, %s defines %d"
            % (GEN_SCORECARDS, len(mirrored), SCORE_GO, len(dims))
        )
    for i, (title, max_) in enumerate(mirrored[: len(dims)]):
        dim = dims[i]
        # The prose shortens two titles ("Network isolation" for "Network
        # isolation / egress"), so match on the leading form, not equality.
        if not dim.title.startswith(title):
            problems.append(
                "canonical-mirror: position %d is %r in %s but %r in %s"
                % (i + 1, title, GEN_SCORECARDS, dim.title, SCORE_GO)
            )
        if max_ != dim.max:
            problems.append(
                "canonical-mirror: %r weighs %d in %s but %d in %s"
                % (title, max_, GEN_SCORECARDS, dim.max, SCORE_GO)
            )
    return problems


# --------------------------------------------------------------------------- #
# Guide parsing.
# --------------------------------------------------------------------------- #

_FRONTMATTER_RE = re.compile(r"\A---\n(.*?)\n---\n", re.S)
_TABLE_HEADER = "| Dimension | Verdict | Score | What the scan found |"
_ROW_RE = re.compile(r"^\|([^|]+)\|([^|]+)\|\s*(\d+)\s*/\s*(\d+)\s*\|(.*)\|\s*$")
_VERDICT_RE = re.compile(r"\b(PASS|FAIL|WARN|UNKNOWN)\b")

# The three sites that state the *default* score unambiguously, plus the body
# prose. "After" totals are phrased differently ("take the same image to",
# "the honest ceiling becomes", "# After:") and are out of scope by design.
_SITE_PATTERNS = {
    "frontmatter title": re.compile(r"scores (\d+)/100 by default"),
    "frontmatter description": re.compile(r"defaults score (\d+)/100(?: \(grade ([A-F])\))?"),
    "before-run comment": re.compile(r"#\s*Before:\s*(\d+)/100(?:, grade ([A-F]))?"),
}
_BODY_SITE_RE = re.compile(r"scores(?: only)? \*\*(\d+) of 100, grade ([A-F])")
_REQUIRED_SITES = ("frontmatter title", "frontmatter description", "before-run comment")


class Guide:
    def __init__(self, path: Path, text: str) -> None:
        self.path = path
        self.text = text
        self.slug = re.sub(r"^harden-|-container-isolation\.md$", "", path.name)
        fm = _FRONTMATTER_RE.match(text)
        self.frontmatter = fm.group(1) if fm else ""
        self.body = text[fm.end():] if fm else text
        # Prose wraps mid-sentence, so score claims straddle newlines.
        self.flat_body = " ".join(self.body.split())
        self.flat_fm = " ".join(self.frontmatter.split())
        image = re.search(r'description:\s*"(\S+) defaults score', self.frontmatter)
        self.image = image.group(1) if image else None


def parse_table(guide: Guide) -> list[dict] | None:
    """The first (and only) dimension table in the guide, or None if absent."""
    lines = guide.text.splitlines()
    try:
        start = next(i for i, ln in enumerate(lines) if ln.strip() == _TABLE_HEADER)
    except StopIteration:
        return None
    rows = []
    for line in lines[start + 2:]:
        if not line.startswith("|"):
            break
        m = _ROW_RE.match(line)
        if not m:
            break
        verdict = _VERDICT_RE.search(m.group(2))
        rows.append({
            "title": m.group(1).strip(),
            "verdict": verdict.group(1) if verdict else m.group(2).strip(),
            "score": int(m.group(3)),
            "max": int(m.group(4)),
            "detail": m.group(5).strip(),
        })
    return rows


def stated_scores(guide: Guide) -> tuple[dict[str, tuple[int, str | None]], list[str]]:
    """Every site that states the default score -> (score, grade letter or None)."""
    sites: dict[str, tuple[int, str | None]] = {}
    missing = []
    haystack = {
        "frontmatter title": guide.flat_fm,
        "frontmatter description": guide.flat_fm,
        "before-run comment": guide.flat_body,
    }
    for name, pattern in _SITE_PATTERNS.items():
        m = pattern.search(haystack[name])
        if not m:
            missing.append(name)
            continue
        letter = m.group(2) if pattern.groups > 1 else None
        sites[name] = (int(m.group(1)), letter)
    body = _BODY_SITE_RE.search(guide.flat_body)
    if body:
        sites["body prose"] = (int(body.group(1)), body.group(2))
    return sites, missing


# --------------------------------------------------------------------------- #
# Measured data.
# --------------------------------------------------------------------------- #

# | [ZooKeeper](harden-zookeeper-container-isolation.md) | 48/100 D | **89/100 B** | ... |
_HUB = Path("docs/blog/hardening-guides.md")
_HUB_ROW_RE = re.compile(
    r"\|\s*\[[^\]]+\]\(harden-(?P<slug>[a-z0-9-]+)-container-isolation\.md\)\s*\|"
    r"\s*(?P<score>\d+)/100\s+(?P<grade>[A-F])\s*\|"
)


def check_hub(hub_src: str, totals: dict[str, int], bands) -> list[str]:
    """The hub's "Before" column restates every guide's total. IRO-696 is the
    precedent for why it needs a gate of its own: an index is prose that claims
    to be exhaustive, and nothing that follows links can see a number in it go
    stale. Checked against the guide's own table, which check_guide() has
    already reconciled against score.go and results.json.
    """
    rows = list(_HUB_ROW_RE.finditer(hub_src))
    if not rows:
        return ["hub: parsed zero guide rows from %s; the table shape changed" % _HUB]
    problems = []
    for row in rows:
        slug, stated, letter = row["slug"], int(row["score"]), row["grade"]
        total = totals.get(slug)
        if total is None:
            problems.append("hub: row for %s has no guide table to check against" % slug)
            continue
        if stated != total:
            problems.append("hub: %s row states %d/100, its guide table sums to %d"
                            % (slug, stated, total))
        if letter != grade_for(stated, bands):
            problems.append("hub: %s row states %d/100 grade %s; %d is grade %s"
                            % (slug, stated, letter, stated, grade_for(stated, bands)))
    return problems


def repo_basename(image: str) -> str:
    """`dpage/pgadmin4:8` -> `pgadmin4`; `mongo:7@sha256:...` -> `mongo`."""
    return re.split(r"[:@]", image, maxsplit=1)[0].rsplit("/", 1)[-1]


def index_scenarios(results: dict) -> tuple[dict[str, dict], dict[str, list[dict]]]:
    by_label, by_repo = {}, {}
    for scenario in results.get("scenarios", []):
        if not scenario.get("label", "").startswith("default-"):
            continue
        by_label[scenario["label"]] = scenario
        by_repo.setdefault(repo_basename(scenario.get("image", "")), []).append(scenario)
    return by_label, by_repo


def resolve_scenario(guide: Guide, by_label: dict, by_repo: dict) -> dict | None:
    """The measured default-configuration run for this guide's image, if any.

    Label first (`default-<slug>` covers 52 of 56). Four guides use a slug the
    survey does not (`cockroachdb` vs `default-cockroach`, `mongodb` vs
    `default-mongo`, `pgadmin4` vs `default-pgadmin`, `victoria-metrics` vs
    `default-victoriametrics`), so fall back to the image repo, and only when
    exactly one scenario claims it.
    """
    scenario = by_label.get("default-" + guide.slug)
    if scenario:
        return scenario
    if not guide.image:
        return None
    candidates = by_repo.get(repo_basename(guide.image), [])
    return candidates[0] if len(candidates) == 1 else None


# --------------------------------------------------------------------------- #
# The check itself.
# --------------------------------------------------------------------------- #

def check_guide(guide: Guide, dims, reachable, bands, scenario) -> list[str]:
    problems: list[str] = []
    rows = parse_table(guide)
    if rows is None:
        return ["no dimension table found (expected a `%s` header)" % _TABLE_HEADER]
    if not rows:
        return ["dimension table header found but zero rows parsed under it"]

    # (2) Dimension integrity: the exact canonical set, in order, at its weights.
    if len(rows) != len(dims):
        problems.append("table has %d rows, the scorer defines %d dimensions"
                        % (len(rows), len(dims)))
    canonical_titles = [d.title for d in dims]
    for row in rows:
        if row["title"] not in canonical_titles:
            problems.append("row %r is not a dimension the scorer emits (canonical set: %s)"
                            % (row["title"], ", ".join(canonical_titles)))
    for title in canonical_titles:
        if title not in [r["title"] for r in rows]:
            problems.append("canonical dimension %r is missing from the table" % title)
    for i, (row, dim) in enumerate(zip(rows, dims)):
        if row["title"] == dim.title and row["max"] != dim.max:
            problems.append("row %r is out of %d, the scorer weights it %d"
                            % (row["title"], row["max"], dim.max))
        elif row["title"] != dim.title and row["title"] in canonical_titles:
            problems.append("row %d is %r, the canonical order puts %r there"
                            % (i + 1, row["title"], dim.title))

    # (3) Verdict reachability: is this (score, verdict) one the scorer can emit?
    by_title = {d.title: d for d in dims}
    for row in rows:
        dim = by_title.get(row["title"])
        if dim is None:
            continue
        pair = (row["score"], row["verdict"].upper())
        if pair not in reachable[dim.key]:
            emits = ", ".join("%d/%s" % p for p in sorted(reachable[dim.key]))
            problems.append("row %r claims %d/%d %s, which %s cannot emit (it emits: %s)"
                            % (row["title"], row["score"], row["max"], row["verdict"],
                               dim.fn, emits))

    # (1) Arithmetic + (4) grade band, at every site that states the total.
    total = sum(r["score"] for r in rows)
    sites, missing = stated_scores(guide)
    for name in missing:
        if name in _REQUIRED_SITES:
            problems.append("no default score stated in the %s" % name)
    for name, (stated, letter) in sorted(sites.items()):
        if stated != total:
            problems.append("%s states %d/100 but the table sums to %d"
                            % (name, stated, total))
        if letter and letter != grade_for(stated, bands):
            problems.append("%s states %d/100 grade %s; %d is grade %s"
                            % (name, stated, letter, stated, grade_for(stated, bands)))

    # (5) Agreement with what the survey actually measured.
    if scenario is None:
        problems.append("no `default-*` scenario in %s for image %s; the table is "
                        "unverifiable against measured data" % (RESULTS, guide.image))
        return problems

    measured = {d["title"]: d for d in scenario["report"]["dimensions"]}
    for row in rows:
        m = measured.get(row["title"])
        if m is None:
            continue
        if (row["score"], row["max"]) != (m["score"], m["max"]):
            problems.append("row %r claims %d/%d, %s recorded %d/%d"
                            % (row["title"], row["score"], row["max"],
                               scenario["label"], m["score"], m["max"]))
        if row["verdict"].upper() != m["verdict"].upper():
            problems.append("row %r claims %s, %s recorded %s"
                            % (row["title"], row["verdict"], scenario["label"], m["verdict"]))
    if total != scenario["score"]:
        problems.append("table sums to %d, %s scored %d"
                        % (total, scenario["label"], scenario["score"]))
    failed = sorted(r["title"] for r in rows if r["verdict"].upper() == "FAIL")
    if failed != sorted(scenario.get("failedDimensions", [])):
        problems.append("table fails %s, %s recorded failures %s"
                        % (failed or "nothing", scenario["label"],
                           sorted(scenario.get("failedDimensions", []))))
    return problems


def read(root: Path, rel: Path) -> str:
    path = root / rel
    if not path.is_file():
        raise ParseError("missing %s" % path)
    return path.read_text(encoding="utf-8")


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("root", nargs="?", default=".", type=Path)
    ap.add_argument("--report", action="store_true",
                    help="print violations and exit 0 (rollout mode)")
    ap.add_argument("--min-guides", type=int, default=DEFAULT_MIN_GUIDES,
                    help="fail if fewer than N guides were parsed (default %d)"
                         % DEFAULT_MIN_GUIDES)
    args = ap.parse_args(argv)
    root: Path = args.root

    try:
        score_go = read(root, SCORE_GO)
        dims = parse_scorers(score_go)
        bands = parse_bands(score_go)
        reachable = parse_reachable(score_go, dims)
        gen_src = read(root, GEN_SCORECARDS)
        results = json.loads(read(root, RESULTS))
    except (ParseError, json.JSONDecodeError) as exc:
        print("error: %s" % exc, file=sys.stderr)
        return 2

    by_label, by_repo = index_scenarios(results)
    findings: list[tuple[str, str]] = []
    for problem in check_mirror(gen_src, dims):
        findings.append((str(GEN_SCORECARDS), problem))

    guides = sorted((root / "docs" / "blog").glob("harden-*-container-isolation.md"))
    offenders = set()
    totals: dict[str, int] = {}
    for path in guides:
        guide = Guide(path, path.read_text(encoding="utf-8"))
        scenario = resolve_scenario(guide, by_label, by_repo)
        rows = parse_table(guide)
        if rows:
            totals[guide.slug] = sum(r["score"] for r in rows)
        for problem in check_guide(guide, dims, reachable, bands, scenario):
            rel = path.relative_to(root) if path.is_relative_to(root) else path
            findings.append((str(rel), problem))
            offenders.add(str(rel))

    hub = root / _HUB
    if hub.is_file():
        for problem in check_hub(hub.read_text(encoding="utf-8"), totals, bands):
            findings.append((str(_HUB), problem))
            offenders.add(str(_HUB))

    print("canonical dimensions: %d (%d pts) from %s"
          % (len(dims), sum(d.max for d in dims), SCORE_GO))
    print("guides parsed: %d" % len(guides))
    print("guides with violations: %d" % len(offenders))
    print("violations: %d" % len(findings))
    for where, problem in findings:
        print("  %s: %s" % (where, problem))

    if len(guides) < args.min_guides:
        print("error: parsed %d guides, expected at least %d -- a guard that reads "
              "nothing passes for the wrong reason" % (len(guides), args.min_guides),
              file=sys.stderr)
        return 2

    if not findings:
        print("OK: every guide's table adds up, matches the scorer and matches the survey.")
        return 0
    if args.report:
        print("(report mode: not failing the build)")
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
