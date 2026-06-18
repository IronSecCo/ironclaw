// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

// Package contract is the frozen seam shared by the control-plane and
// the sandbox. It is the ONLY package both sides import.
//
// Everything here — typed IDs, enums, row structs, the embedded SQL schema, the
// crypto open helpers, the interface-segregated queue access, and the gateway
// protocol — is pinned so that the host and the sandbox compile against byte
// identical types and byte identical crypto parameters. A drift here is a silent
// decrypt failure or a routing mismatch at runtime, so the package is frozen.
//
// Changes require a joint RFC entry in docs/contract.md and approval from BOTH
// CODEOWNERS (the control-plane owner and the sandbox owner). Neither agent may
// edit this package unilaterally.
package contract
