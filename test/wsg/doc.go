//go:build wsg_verify

// Package wsg is the WS-G Linux CI live-verification harness (IRO-93).
//
// It exercises the no-external-secret rows of the WS-G 1.0 capability gate
// (see the verification runbook on IRO-84) as real, executing assertions rather
// than spec review:
//
//   - G2 isolation     — the real isolation code builds a network=none OCI bundle;
//     the real egress broker allows only approved hosts over a
//     bound unix socket and audits denials; and (when runsc is
//     installed in CI) a real sandbox launches under gVisor and
//     proves only `lo` is present with internet egress blocked.
//   - G8 skills/a2a/sched — a minisign-signed skill bundle is gated at the real
//     gateway (deny-by-default, approved install applies);
//     create_agent spawns a gated child; a near-term scheduled
//     task fires once through the real queue-backed sweep.
//   - G5 webhook/SMTP  — the real Webhook and Email adapters round-trip against an
//     in-process HTTP receiver and an in-process SMTP sink.
//
// The whole package is behind the `wsg_verify` build tag so it is EXCLUDED from
// the repository's required `go test ./...` gate (it must never be able to break
// release) and only runs in the dedicated, additive `wsg-verify` workflow, or
// locally via `go test -tags wsg_verify ./test/wsg/...`.
//
// Everything except the live runsc launch runs on any OS, so the bulk of the
// harness is verifiable off-CI; the runsc-gated launch self-skips when `runsc`
// (or the CI-staged rootfs/probe) is absent so the job stays green by skipping
// rather than failing on a host that cannot run gVisor.
package wsg
