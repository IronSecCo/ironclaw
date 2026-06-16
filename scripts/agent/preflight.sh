#!/usr/bin/env bash
# IronClaw agent preflight — run before every direct push to main (CAS mode).
# All checks use the real toolchain: Go 1.23+ with CGO_ENABLED=1 (SQLCipher binding).
set -euo pipefail

export CGO_ENABLED=1

echo "== IronClaw agent preflight =="
echo "Git SHA:   $(git rev-parse HEAD)"
echo "Base main: $(git rev-parse origin/main 2>/dev/null || echo '(no origin/main)')"

echo "== Changed files vs origin/main =="
git diff --name-only origin/main...HEAD || true

echo "== gofmt -l . (format) =="
unformatted="$(gofmt -l . 2>/dev/null || true)"
if [ -n "$unformatted" ]; then
  echo "FAIL: these files need gofmt:"
  echo "$unformatted"
  exit 1
fi

echo "== go vet ./... (lint/typecheck) =="
go vet ./...

echo "== go build ./... =="
go build ./...

echo "== go test ./... =="
go test ./...

echo "== Preflight passed =="
