---
title: "One of our container images carries two green provenance statements. Only one of them is true."
description: "We published a signed SLSA provenance statement that verifies green and is false. Here is the digest, why it cannot be retracted, and what to check on your own release pipeline."
---

# One of our container images carries two green provenance statements. Only one of them is true.

We publish signed SLSA build provenance for every IronClaw release image. On 2026-07-29 we
published a statement that is correctly signed, correctly identified, logged in the public
transparency log, verifies green with the standard tool, and is false. It credits a build
with producing an image that build never touched.

Here is the digest, so you can check the whole story yourself without taking our word for
any of it:

```
ghcr.io/ironsecco/ironclaw-controlplane@sha256:be66da22455e11b8625693a29535f599500c126165eeabb34775492a962ce5b7
```

That is the real `v0.1.407` control-plane index. It carries four attestations: two SLSA v1
provenance statements and two CycloneDX SBOMs. One provenance statement is from the run that
actually built it. The other is from a later, unrelated run that built something else.

We cannot retract it. It will be verifiable and wrong for as long as the transparency log
exists. This post is what we learned taking that apart, and it is written for the person who
runs their own release pipeline, because the primitive that caused it is in extremely wide
use.

## What actually went wrong

The build step and the attestation step are two different steps, and nothing structurally
connects them.

`actions/attest-build-provenance` takes the artifact digest as an ordinary input and signs
whatever digest it is handed. It does not observe your build. It does not know what your
build produced. If the value you pass it is wrong, you get a perfectly valid attestation
about the wrong artifact. That is the primitive, and it is not a bug in the action. It is
what the action is.

Our wrong value came from a tag. Our merge job resolved the index digest it had just
published by reading back a hardcoded `:latest`. Separately, we had made `:latest` move
forwards only, so a build of a commit that has fallen behind the branch tip publishes its
immutable `:<version>` tag alone and never touches `:latest`. On the first run where those
two facts met, the job read a digest it had not pushed, six minutes of drift between two
workflows being all it took, and then attached both the provenance and the SBOM to that
older index.

The flip side is worth stating too, because it is the quieter half. The image that run did
build, `sha256:19a65817404bb4feebabec3fe05840301072679c4b6ec0f8a5d62d70160d3478`, went out
with no provenance and no SBOM at all. Query the attestations API for it today and you get
HTTP 404. So one release produced an unattested artifact and a false attestation on a
different artifact, from the same mistake.

## Why it verifies green

Run the standard check against the bad digest:

```sh
gh attestation verify \
  oci://ghcr.io/ironsecco/ironclaw-controlplane@sha256:be66da22455e11b8625693a29535f599500c126165eeabb34775492a962ce5b7 \
  --repo IronSecCo/ironclaw
echo $?
```

In CI, where stdout is not a terminal, gh 2.95.0 exits 0 and prints nothing at all. Run the
same command in a terminal and it will tell you it matched 2 attestations and list both, and
still exit 0 without noting that they name different builds.

This is deliberate. GitHub made attestation verification monotonic on purpose, and monotonic
is their word, not ours: once verification passes for an artifact, adding another attestation
cannot change that. Verification succeeds if at least one attestation passes, so that someone
attaching a bad or malformed extra attestation cannot break gated deployments for a
legitimate build. That is a reasonable design against the threat they modelled. The case it
leaves uncovered is the one we hit. Our second statement is not malformed at all. It is a
valid, GitHub-signed provenance from the same repo and the same workflow file, and it passes
every check the CLI makes. It is simply about a different build, and nothing in the CLI ever
compares two verified statements against each other.

The same shape shows up elsewhere. `slsa-verifier`, which reads cosign-style attestations
rather than GitHub's, returns the first candidate that verifies, and it does not choose the
order: candidates arrive in the layer order of the attestation manifest the registry serves,
which the verifier neither controls nor normalises. `cosign verify-attestation` applies its
policy over all matching statements, but signature verification underneath is a logical OR,
so a statement it cannot attribute is dropped before policy ever sees it.

Ask for the run IDs explicitly and the problem is visible immediately:

```sh
gh attestation verify oci://<image>@<digest> --repo <owner>/<repo> --format json \
  | jq -r 'length as $n | "statements: \($n)",
      (.[].verificationResult.statement
       | "  \(.predicate.runDetails.metadata.invocationId)  subject=sha256:\(.subject[0].digest.sha256)")'
```

Two provenance statements, two different run IDs, on our digest.

## Why it cannot be retracted

Attestations are keyed by digest, the store is append-only, and the transparency log entry is
immutable.

We checked the obvious escapes and none of them work. Cutting a new release does not help:
it moves the tag, not the statement. Our `:latest` has long since moved on to healthy
releases, and `sha256:be66da22` still carries both statements. Deleting the package version
does not help either: it removes a distribution point, not a signed and logged claim, and it
destroys the evidence an auditor would need to reason about the artifact.

So we retained it. It is not a bad artifact. It is the genuine `v0.1.407` index, the one our
Homebrew formula tracked, and it happens to be the only live specimen anybody has of this
failure mode. The remediation for the statement itself could only ever be documentation, and
documentation of this kind is worthless unless it is a check rather than a claim.

## How an outsider can tell the two statements apart

Two independent checks. Both are runnable by someone with no access to our logs, our repo
settings, or our accounts. Both would work on your pipeline as well as ours.

**1. Immutable version-tag correspondence.** Every release run publishes an immutable
`:<version>` tag naming the index it built. A statement claiming a subject must come from a
run whose own `:<version>` resolves to that same subject. Ours: `:v0.1.407` resolves to
`be66da22`, which is the subject, so that statement is real. `:v0.1.411` resolves to
`19a65817`, which is not the subject, so the statement from that run is false.

**2. Build time against the run window.** The amd64 image config `created` timestamp was
`00:01:44Z`, and the first statement was logged at `00:02:18Z`. The run that produced the
second statement started at `00:46:08Z`, forty-four minutes after the artifact it claims to
have built was already signed and logged. A build cannot produce an artifact that
demonstrably already existed. That is causality, not a heuristic, and it is the strongest
single signal we found.

One field that looks like it should work does not. `resolvedDependencies[].digest.gitCommit`
is the commit the workflow checked out for its trigger event, which for a `workflow_run`
pipeline is the branch tip, not the commit that was built. On our own false statement that
field names a commit the run did not build either. Do not use it to identify a build.

## What we changed, and what shipped since

Fixed at the source. Nothing in our image workflow resolves a digest by reading back
`:latest` any more. We still publish the tag, but only when the commit being built is the
default-branch tip, and no step derives anything from it. The prepare job emits one list of
the tag names the run publishes, immutable `:<version>` first so the lookup goes through a
tag a concurrent run cannot move, and every downstream step derives from that list. The merge
job now asserts that every tag it applied resolves to the one index it is about to attest, so
a digest the run did not build can no longer become an attestation subject silently. A digest
header that cannot be read is now a failure rather than a skip.

Documented as a check. Our release runbook now says in as many words that a green
`gh attestation verify` is necessary and not sufficient, and gives the two commands above.
The incident, the digest, the disposition, and a section titled "what could not be
retracted" are all written down, so nobody later reads "superseded" as "cleaned up".

Then we measured the detection rule before building anything on it, because a rule that fires
on healthy pipelines is worse than no rule at all.

Three obvious rules died on real data, measured across 3,398 attested digests from our own
registry and two third-party publishers:

- "More than one attestation is suspicious" fires on 173 of our own 185 attested digests,
  because provenance plus SBOM is the normal healthy shape. Dead on arrival.
- "More than one provenance statement is suspicious" fires on 2,121 of 3,213 third-party
  attested digests, all benign. The mechanism is duplicate storage rather than anything
  anomalous: one release tool writes four byte-identical bundles from a single run, another
  writes two, and the bundles agree on every field.
- "Provenance statements disagree on source commit" had a zero false-positive rate on the
  third-party sample and is still wrong, for the `gitCommit` reason above. We rejected it on
  correctness rather than on measurement.

What survives: two or more provenance statements from two or more distinct run IDs, after
normalising `runs/<id>/attempts/N` down to the run ID and counting only statements whose
predicate type is SLSA provenance. Across all 3,398 attested digests that fires once, on the
known-true incident, with zero false positives. The normalisation is load-bearing rather than
cosmetic: without it the same rule reports six false positives, every one of them a
legitimate re-run that re-attested an identical digest under a later attempt of the same run.

Now the part we shipped, stated with its limits. We built the detector into our own release
pipeline as a post-publish gate: after the image is published, the release fails unless the
digest carries exactly one SLSA provenance statement and that statement was minted by the run
that is publishing it. We also stamp `org.opencontainers.image.revision` and
`org.opencontainers.image.source` onto the image and assert them against the trust-gated
build commit before anything is attested, so the image now carries an independent claim about
its own origin that a wrong digest cannot fake. It is not a plan. It has run green on two
shipped releases, `v0.1.418` and `v0.1.419`, asserting on each published index that the
digest carries exactly one provenance statement and that the run which minted it is the run
publishing it.

One limit on that, stated before someone else states it for us: passing is not catching. Both
of those releases were already clean, so what the gate has demonstrated in production is that
it does not false-positive on a healthy release. Its true-positive behaviour is evidenced by
replaying it against the run IDs from this incident, not by a live catch.

We did not ship it as something you run. There is no consumer-facing verifier here, and
deliberately so, for the reason in the next section.

Three caveats we would rather state than have someone find:

**This does not belong in admission control.** The rule is non-monotonic by construction:
adding a statement flips allow to deny. An attestation bundle is not authenticated as a set,
so whoever can add a statement can usually also delete one, and deleting the genuine
statement disarms the check. It is sound exactly where the set is authoritative and
non-deletable, which means the publisher's own CI immediately after publish, and auditors
reading an append-only log. Not your cluster's admission webhook.

**Coverage is thinner than it looks.** Most publishers we looked at do not use GitHub
Artifact Attestations at all. They sign with keyless cosign and attach statements as OCI
referrers or as a cosign attestation tag, none of which the GitHub attestations endpoint can
see. Even among publishers who do use it, adoption is partial rather than universal: one of
the two we measured in full carries GitHub attestations on 87 of its 656 published digests.
A check built only on that endpoint silently passes most of the ecosystem without ever having
looked.

**The honest false positive is bit-reproducible images**, where two runs genuinely produce
the same digest. That is narrower than it sounds, and our own repo is the proof: our CI hard
asserts bit-for-bit reproducible binaries, the two commits here differed only in CI, docs
and packaging files so the binaries were identical, and the two runs still produced different
index digests. Reproducible binaries do not imply reproducible images.

## What to go check on your own pipeline

Five minutes, and no account needed for the first one. The public per-repository attestations
endpoint answers unauthenticated:

```sh
curl -s "https://api.github.com/repos/<owner>/<repo>/attestations/sha256:<digest>" | jq '.attestations | length'
```

1. Count the statements on your latest release digest. If the number surprises you, start
   there.
2. Check that every provenance subject is the digest the claiming run actually published,
   not just a digest that exists.
3. Look for any step that resolves a digest by reading back a mutable tag. That is the
   pattern. Ours was `:latest`.
4. Check whether a conditional publish left some later step reading a stale answer and
   calling it success. When a publish becomes conditional, every step that reads the result
   back has to learn the condition.
5. Confirm the artifact you actually shipped has provenance at all. The false attestation was
   loud once we looked. The missing one on the real artifact was completely silent.

We build supply-chain security tooling, and we found this on ourselves. The reason it is
worth publishing is not that the outcome was bad, it is that under the standard tooling the
outcome looked fine. If your pipeline hands a digest to a signer as a plain input, and
something upstream can get that digest wrong, you have the same primitive we do.

The check itself, with the copyable YAML, the base rate we measured and where the build
identifier lives in each provenance shape, is in
[Appendix: assert your own build provenance in CI](assert-your-own-build-provenance.md).
