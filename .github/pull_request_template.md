<!--
Thanks for contributing to IronClaw! Keep changes small, focused, and reversible,
and link the issue this addresses.
-->

## What & why

<!-- One or two sentences: what this changes and the motivation. -->

Closes #

## Changes

- <!-- bullet the concrete changes -->

## Checklist

- [ ] **Tests pass:** `CGO_ENABLED=1 go test ./...` is green (CGO is required — SQLCipher binding).
- [ ] **Formatted & vetted:** `gofmt -l .` is empty and `go vet ./...` passes.
- [ ] **Frozen contract:** `internal/contract/**` is **untouched** — or this carries an approved joint RFC (`docs/contract.md`) with code-owner sign-off.
- [ ] **Threat model:** no change to the sandbox seal / `network=none` / approval-gateway posture — or it is called out and reviewed.
- [ ] **Docs updated:** README / `docs/**` reflect any user-facing or behavioral change.
- [ ] **No secrets:** no tokens, keys, or credentials in code, tests, fixtures, or logs.

## Validation

```bash
CGO_ENABLED=1 go test ./...
```

<!-- Paste relevant output, or describe manual verification (e.g. `--dev` boot, endpoint check). -->
