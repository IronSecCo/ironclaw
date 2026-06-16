# The frozen contract

`internal/contract` is the single seam shared by the control-plane (AGENT1) and
the sandbox (AGENT2). It is the **only** package both sides import, and it is
**frozen**.

## The freeze rule

Once the skeleton lands, neither agent may edit `internal/contract/**`
unilaterally. Every file there carries the banner:

```
// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).
```

A drift in this package is not a build error — it surfaces at runtime as a silent
decrypt failure (mismatched cipher params) or a routing mismatch (mismatched row
shapes). That is why the freeze is strict.

## The RFC process

To change the contract:

1. **Write an RFC.** Append a new dated section to the "RFC log" below describing
   the change, the motivation, and the migration impact on both trees.
2. **Get both CODEOWNERS to approve.** The control-plane owner and the sandbox
   owner must both sign off. This is enforced by `CODEOWNERS`, which lists
   `/internal/contract/` with both required reviewers.
3. **Land the contract change and both implementations together** so the host and
   sandbox never compile against divergent types or crypto params.

Pinned crypto parameters (`CipherScheme`, `CipherPageSize`, `KDFRawKey`) and seq
parity (host=even, sandbox=odd) are part of the contract and follow the same rule.

## RFC log

_None yet. The initial contract was authored with the skeleton._
