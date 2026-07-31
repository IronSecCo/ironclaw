---
title: "Appendix: assert your own build provenance in CI"
description: "The assertion we run after publishing an image, where the build identifier lives in each provenance shape, the base rate we measured, and copyable YAML."
---

# Appendix: assert your own build provenance in CI

This is the technical half of
[One of our container images carries two green provenance statements. Only one of them is true](two-green-provenance-statements.md).
That post is the story. This page is the check.

There is nothing to install and nothing here that we host. It is about thirty lines of YAML
that runs in the workflow that publishes your image, right after the push. We are publishing
the assertion rather than a tool on purpose: the attestation ecosystem has three carriers,
several predicate shapes and two registries that disagree about whether the referrers API
exists, and a tool would have to promise to track all of that forever. A snippet you own is
honest about who is holding it.

## The failure mode, in brief

`actions/attest-build-provenance` takes an artifact digest as an ordinary input and signs
whatever digest it is handed. It does not observe your build. If the value you pass it is
wrong, you get a correctly signed, correctly identified, publicly logged attestation about
an artifact that build never touched. Ours came from a job that resolved the digest it had
just published by reading back a mutable tag. The full story, including why it cannot be
retracted and why the standard verifier is green on it, is in the write-up.

The check below closes the gap that made it invisible: nothing in the verification path ever
compares two verified statements against each other.

## What this checks, and what it does not

On the tin, in full:

> Checks SLSA provenance statements published as GitHub Artifact Attestations or as cosign
> `.att` tags. Does not apply to BuildKit inline attestations, where statements are part of
> the image digest and this class of defect cannot occur. Reports, and does not pass, images
> where no provenance is present or where the provenance carries no build identifier.

Two things worth spelling out.

**BuildKit inline attestations are out of scope because the defect is structurally
impossible there.** Those statements are children of the image index, so they are part of
the digest. You cannot append a conflicting provenance to an existing digest; adding one
produces a different digest and leaves the old one untouched. The rule is not unsupported
there, it is vacuous. In the sample we measured, that is the majority of images.

**A digest can carry statements in more than one carrier at once.** Two of the publishers we
looked at carry BuildKit inline attestations and GitHub Artifact Attestations on the same
digest. A check that reads one carrier has asserted a property of a subset while reporting it
as a property of the artifact, which is the same defect class as the incident. If you run the
snippet below, you are checking the GitHub Artifact Attestations on that digest, not
everything anybody has ever said about it.

## Three outcomes, not two

| Outcome | Meaning | Default behaviour |
|---|---|---|
| PASS | Provenance is present, every statement comes from one run, that run is this run, and every subject is this digest or one of its platform children | Step is green |
| FAIL | Two or more distinct runs, a run that is not this one, or a subject this run did not publish | Warning annotation and a job summary line. Exits non zero only if you opt in |
| NOT-EVALUABLE | No provenance found on this carrier, provenance that carries no comparable build identifier, or a query that did not answer | Warning annotation and a job summary line. Never a pass |

NOT-EVALUABLE must never render as green. It is the honest answer for most of the ecosystem
and treating it as a pass is how a check ends up certifying nothing while looking like it
certified something.

## The assertion

Over the set S of statements on digest D whose predicate type matches
`https://slsa.dev/provenance/`:

- **A1** `|S| >= 1`. Zero provenance is not a pass. In your own publishing workflow, where
  you know you attested, treat it as a failure; when you are looking at somebody else's
  image it is NOT-EVALUABLE.
- **A2** the set of normalised run identifiers `R` is a singleton.
- **A3** `R == { GITHUB_RUN_ID }`.
- **A4** every statement in S names D, or a child manifest of D, as a subject, matched on
  the digest hex only. Subject names are not portable: one publisher we measured names its
  subjects `kubectl` and `kubectl - linux/amd64`.

The rule is **one distinct run, and it is mine**, not one statement. Our own gate asserts
exactly one statement, which is correct for our pipeline's shape and wrong in general. Two
counter examples, both re-verified live on 2026-07-29:

```text
ghcr.io/goreleaser/goreleaser:latest   4 provenance statements, 1 distinct run
ghcr.io/astral-sh/uv:latest            2 provenance statements, 1 distinct run
```

Byte identical duplicate storage from a single healthy run. A count based rule fails both.
A2 plus A3 accepts both, and it is strictly stronger than any multiplicity heuristic, because
in your own publishing workflow you know your own run identifier.

## Where the build identifier lives

This is the fiddly part, and it is different in every shape. All five rows were read off live
statements on 2026-07-29 rather than from documentation.

| Provenance shape | Field carrying the build identifier | Normalisation | Evaluable |
|---|---|---|---|
| GitHub Artifact Attestations, SLSA v1, `actions/attest-build-provenance` | `predicate.runDetails.metadata.invocationId`, of the form `https://github.com/OWNER/REPO/actions/runs/<id>/attempts/<n>` | strip the trailing `/attempts/<n>`, take the run id | Yes |
| cosign `.att` tag, SLSA v0.2, `slsa-github-generator` | `predicate.invocation.environment.github_run_id`, with the attempt in a separate `github_run_attempt` | none needed | Yes |
| BuildKit inline, SLSA v0.2 | `predicate.builder.id`. Ends `/actions/runs/<id>` for some publishers, is an organisation URL with no run id for others, and is empty for others again | take the trailing run id when one is present | Out of scope. Not evaluable when the identifier is absent |
| BuildKit inline, SLSA v1 | `predicate.buildDefinition.internalParameters.github_run_id`, attempt alongside it. `runDetails.builder.id` is empty and `invocationId` is a BuildKit local opaque string, not a run | none needed | Out of scope |
| SLSA v1 from a non GitHub builder | `predicate.runDetails.metadata.invocationId` is present but is a build system UUID, and `predicate.runDetails.builder.id` is a vendor documentation URL | no mapping to a GitHub run exists | **No.** NOT-EVALUABLE, and it must not be counted as a pass |

The last row is the one that decides the shape of the whole check. An identifier being
present is not the same as an identifier being comparable, and a check that quietly treats an
uncomparable identifier as a match is worse than no check.

The `attempts/<n>` normalisation is load bearing rather than cosmetic. `GITHUB_RUN_ID` is
stable across attempts, so a re-run that re-attests an identical digest under a later attempt
is legitimate. Without the normalisation the same rule reported nine false positives on our
measurement set, every one of them exactly that: one run identifier, two or three attempts,
an identical digest re-attested under each.

## The base rate, measured before we trusted the rule

A rule that fires on healthy pipelines is worse than no rule. What the assertion does on real
data, across two independent measurements:

- **GitHub Artifact Attestations path: 4,783 attested digests** (195 ours, 4,588 from third
  party publishers). 3,031 of the third party digests carry more than one provenance
  statement, all benign duplicate storage. After run identifier normalisation, the assertion
  fires **once, with zero false positives**, and the one fire is the incident this appendix
  is about.
- **cosign `.att` path: 829 digests swept, all 829 decoded to statement level, 891 statements,
  zero fires.** Two of those three publishers were swept in full; the third was sampled at a
  fixed stride across its 4,636 `.att` tags rather than read end to end. One of them attests
  SBOMs only and carries no provenance at all, so the assertion is inapplicable rather than
  passing, which is exactly the case the NOT-EVALUABLE outcome exists for.

Those are a snapshot taken on 2026-07-29, and for numbers like these the method matters more
than the totals. Every tag list was paginated to exhaustion rather than read one page deep,
tags were resolved to digests and deduplicated before anything was counted, and every response
was classified rather than coerced: a 404 from the attestations endpoint is a real absence, any
other non-200 is a query that did not answer, and those were retried until they did. Nothing
was excluded. An earlier pass at this same measurement dropped 1,765 digests whose probes came
back as the endpoint's own secondary rate limit, and it undercounted every figure above as a
result. Repeat it later and you should expect larger numbers, because these publishers keep
publishing.

We have not found a single legitimate multi run provenance set in any dataset.

Carrier adoption, from a hand assembled probe of public images on GHCR and Docker Hub:
BuildKit inline attestations are the common case by a wide margin, a cosign `.att` tag is
uncommon, GitHub Artifact Attestations are rarer still, and a substantial minority carry none
of the three. The counts overlap, because some digests carry two carriers. We are deliberately
not publishing exact ratios for this one. The candidate list was assembled by hand rather than
sampled from anything, `latest` moves under it, and a later re-probe of the same images found a
BuildKit carrier on one that the first pass had left unresolved. The ordering is the finding;
treat the ratio as indicative.

One mechanical finding that changes how you enumerate: **GHCR does not implement the OCI
referrers API.** `/v2/<repo>/referrers/<digest>` returned HTTP 404 on every GHCR repository
we probed, including repositories that demonstrably do have cosign artifacts. Docker Hub
answered 200 on every repository we probed. We give the outcome rather than a repository
count because the candidate list was hand assembled like the one above, but one point of
method is worth stating, because both answers carry the same status code: a repository that
returned no pull token or no `latest` tag was excluded rather than scored as a 404. Only
repositories that first answered 200 to a manifest fetch were scored at all. On GHCR, the
referrers path in practice is the cosign fallback tag scheme `sha256-<hex>.att`.

## The snippet

Add this to the job that publishes the image, after the push step. `DIGEST` is the digest
that step reported.

`IMAGE`, `DIGEST` and `FAIL_ON` are the three values you set. Four things it assumes about
the job around it, none of which it can check for you:

- **A push step whose digest you can reference.** `steps.push.outputs.digest` is the id of
  our push step, not a builtin. Point it at yours.
- **`attestations: read` in the job's `permissions`.** `github.token` carries it under the
  permissive default, but not if your repository or workflow narrows permissions, which is
  the setting we would recommend anyway.
- **`gh`, `jq` and `docker buildx` on the runner.** All three are present on GitHub hosted
  runners. On a self hosted runner or inside a container job you may have none of them.
- **Registry access for a private image.** Both `gh attestation verify` and `imagetools`
  read the registry. If the package is private, the job needs to be logged in to it already.

It writes `att.json` and `att.err` into the working directory, so run it after any step that
cares about a clean tree.

```yaml
- name: Assert published provenance is single run and mine
  env:
    IMAGE: ghcr.io/OWNER/NAME                  # no tag
    DIGEST: ${{ steps.push.outputs.digest }}   # sha256:...
    GH_TOKEN: ${{ github.token }}
    FAIL_ON: ""                                # set to "multi-run" to make FAIL red
  run: |
    set +e -uo pipefail
    say()  { echo "$1"; echo "$1" >> "${GITHUB_STEP_SUMMARY:-/dev/null}"; }
    warn() { echo "::warning::$1"; say "$1"; }
    if ! gh attestation verify "oci://${IMAGE}@${DIGEST}" --repo "${GITHUB_REPOSITORY}" \
         --format json > att.json 2> att.err; then
      warn "provenance NOT-EVALUABLE on ${DIGEST}: $(tail -n1 att.err)"; exit 0
    fi
    read -r n runs subs <<EOF
    $(jq -r '
    def runid:
      (.predicate.runDetails.metadata.invocationId
       // .predicate.invocation.environment.github_run_id // "") | tostring
      | sub("/attempts/[0-9]+$"; "")
      | if test("^[0-9]+$") then . elif test("/actions/runs/[0-9]+$")
        then capture("/actions/runs/(?<i>[0-9]+)$").i else "?" end;
    [ .[].verificationResult.statement
      | select(.predicateType | startswith("https://slsa.dev/provenance/")) ]
    | "\(length) \([.[]|runid]|unique|join(",")|if .=="" or test("[?]") then "-" else . end) \([.[].subject[].digest.sha256]|unique|join(","))"
    ' att.json)
    EOF
    case "${n:-}" in ''|0) warn "provenance NOT-EVALUABLE on ${DIGEST}: no SLSA provenance statement"; exit 0;; esac
    [ "$runs" = "-" ] && { warn "provenance NOT-EVALUABLE on ${DIGEST}: ${n} statement(s), not every one carries a comparable build identifier"; exit 0; }
    kids=$(docker buildx imagetools inspect "${IMAGE}@${DIGEST}" --raw | jq -r '[.manifests[]?.digest]|join(" ")' | sed 's/sha256://g') ||
      { warn "provenance NOT-EVALUABLE on ${DIGEST}: could not read the image index to resolve platform children"; exit 0; }
    allowed=" ${DIGEST#sha256:} ${kids} "
    stray=""; for s in ${subs//,/ }; do [[ "$allowed" == *" $s "* ]] || stray="$stray $s"; done
    if [ "$runs" = "${GITHUB_RUN_ID}" ] && [ -z "$stray" ]; then
      say "provenance PASS on ${DIGEST}: ${n} statement(s), one run (${runs}), every subject matches"; exit 0
    fi
    warn "provenance FAIL on ${DIGEST}: runs=[${runs}] expected ${GITHUB_RUN_ID} unexpected-subjects=[${stray# }]"
    [ "${FAIL_ON:-}" = "multi-run" ] && exit 1 || exit 0
```

Five notes on why it is written the way it is.

- **`set +e` is the first line, and it is not a typo.** A `run:` step with no `shell:` key
  runs under `bash -e`, so any unchecked command failure ends the step before the check can
  report anything. This step decides its own exit code, so it turns that off deliberately.
  Delete that and a slow registry ends your build with no verdict at all.
- **It reads the JSON, not the exit code.** `gh attestation verify` exits 0 if at least one
  statement verifies, and when stdout is not a terminal it prints nothing at all. Both are
  deliberate on GitHub's side, and both are why a green verify did not save us.
- **`gh attestation verify` filters to SLSA provenance v1 by default.** If you mint v0.2
  through GitHub Artifact Attestations, pass `--predicate-type` explicitly, or the check will
  correctly report NOT-EVALUABLE on statements it was never shown.
- **The `imagetools` call is what makes A4 usable on a multi platform image.** Provenance can
  be attached per platform, in which case a per platform child digest is a legitimate subject
  and the check has to say so rather than flagging it. Worth being precise about how much work
  this branch does: on all 4,783 attested digests we measured, every provenance statement named
  the index digest itself and not a child, so we have never actually seen it fire. It is here
  because the shape is legal and cheap to accept, not because it is common.
- **Either query failing is NOT-EVALUABLE, not FAIL.** The attestations endpoint has its own
  secondary rate limit, and a 403 from it means you did not get an answer, not that the
  answer was bad. The same goes for the registry read: if the index cannot be fetched, the
  check does not know which platform children are legitimate subjects, so it says so rather
  than reporting subjects it could not resolve as strays.

We replayed the snippet against every branch it has, with `gh` and `docker` answering from
fixtures: the incident digest (FAIL, two runs), goreleaser and uv shapes (PASS,
four and two statements from one run), a mismatched run id (FAIL), a statement naming a
digest the run did not publish (FAIL), a legitimate platform child subject (PASS), no
attestations at all (NOT-EVALUABLE), a statement whose identifier is a non GitHub UUID
(NOT-EVALUABLE), a set mixing a real run id with an uncomparable one (NOT-EVALUABLE), and a
registry that refused the index read (NOT-EVALUABLE). Every case was run under `bash -e`,
the runner's own default, because that flag decides the outcome of three of them. With
`FAIL_ON` unset every case exits 0.

Six of those branches were then run again with nothing stubbed at all, against live published
images: the incident digest FAILs and exits 0, the same digest with `FAIL_ON: multi-run` exits
1, a current single run release of ours PASSes, that same release with a mismatched run id
FAILs, an image carrying only BuildKit inline provenance comes back NOT-EVALUABLE and green,
and a registry that will not serve the index comes back NOT-EVALUABLE and green even with
`FAIL_ON: multi-run` set. The one branch we cannot reach from live data is a statement that
verifies but carries no comparable identifier, because publishers whose identifiers look like
that are not in GitHub's attestation store to begin with.

### The cosign path

If you publish statements as a cosign `.att` tag rather than through GitHub, the same
assertion applies with a different fetch. The `.att` manifest's layer list **is** the
statement set, so it is one registry GET with no pagination: resolve your digest, request
the tag `sha256-<hex>.att`, then for each layer blob base64 decode `.payload` and apply A1
to A4 to what comes out. Run identifiers there are usually the v0.2 shape in the table above.

One caveat you should carry with it: GitHub Artifact Attestations are append only, but
`sha256-<hex>.att` is an ordinary registry tag and anyone with push access can replace or
delete it. The same check proves strictly less on that path, and it is worth saying so
rather than describing the two as equivalent.

## Warn by default. Our own gate does not, and that is deliberate

The snippet defaults to annotate and warn. `FAIL_ON: multi-run` is opt in.

Our own release pipeline runs the strict version: it fails closed, and a query error fails
the release. That is the right trade for us and we are deliberately not shipping it to anyone
else as a default, for three reasons.

1. **Fail closed means somebody else's rate limit can fail your release.** The
   attestations endpoint's secondary rate limit has already turned one of our releases red.
   That was correct behaviour for us, from our own code. Shipping it as a default means our
   snippet failing your release over a 403 you cannot see.
2. **A fire is unfixable in place.** The attestation log is append only. If this assertion
   fires, correctly or not, there is no way to retract the extra statement. The only remedy
   is to rebuild and republish under a new digest, which is a large thing to trigger by
   default from a snippet somebody copied.
3. **The realistic false positive is legitimate republication.** A publisher who attests the
   same digest from a second workflow, or promotes a previously built digest in a later
   release, trips A2 and A3 while doing nothing wrong. We did not observe this in any
   dataset, and nothing in the data rules it out either.

Turn `FAIL_ON` on once you have watched it stay quiet across a few releases of your own.

## Where this belongs, and where it does not

**This does not belong in admission control.** The assertion is non monotonic by
construction: adding a statement flips allow to deny. An attestation set is not authenticated
as a set, so whoever can add a statement can usually also delete one, and deleting the genuine
statement disarms the check. It is sound exactly where the set is authoritative and cannot be
deleted, which means the publisher's own CI immediately after publish, and auditors reading an
append only log. Not your cluster's admission webhook.

It is also a detector, not a preventer. By the time it can run, the digest is public. In a
publishing pipeline that is still useful, because you can stop the release from advertising
it. It will not stop the statement existing.

## What we did not measure

Stated plainly, because the numbers above are worth exactly what their method is worth:

- Adoption on registries other than GHCR and Docker Hub.
- Whether any publisher legitimately attests one digest from two different workflows. This is
  the one false positive we expect to exist and have never seen.
- The cosign path base rate beyond the three publishers we swept, two of them in full and the
  third as a fixed stride sample across its tag list rather than all of it.
- Whether this assertion catches a real regression in production. Our own gate has run green
  on shipped releases, which demonstrates the absence of false positives and nothing more.
  Passing is not catching. Its true positive behaviour rests on replay against the run
  identifiers from this incident.
