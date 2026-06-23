// Package smoke holds hermetic end-to-end smoke tests that exercise IronClaw's
// headline flows across package boundaries without any network, credential, or
// Docker dependency.
//
// The flagship test here protects the zero-credential chat demo — the README
// hero onboarding path — by driving a real encrypted inbound→sandbox-loop→
// outbound round trip with the offline mock provider, so the demo cannot
// silently break. Keeping it in its own package (rather than inside host/ or
// sandbox/) lets a single test legitimately wire together the host queue
// (inbound writer / outbound reader) and the sandbox tree (loop + provider +
// queue) the way the running daemon does, while respecting each tree's import
// discipline in non-test code.
package smoke
