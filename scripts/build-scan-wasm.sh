#!/usr/bin/env bash
# build-scan-wasm.sh — compile the pure containment scorer to WebAssembly for the
# public web scanner (IRO-598) and stage it next to the Go runtime's wasm_exec.js.
#
# The .wasm binary embeds internal/host/scan as-is, so the in-browser grade can
# never drift from `ironctl scan`. Re-run this whenever the scorer changes and
# copy the two artifacts into the landing repo's public/scan/ directory.
#
# Usage:
#   scripts/build-scan-wasm.sh [OUT_DIR]
#
# OUT_DIR defaults to ./dist/scan-wasm. Pass the landing repo's public/scan path
# to build straight into place, e.g.:
#   scripts/build-scan-wasm.sh ../nivardsec-landing/public/scan
set -euo pipefail

OUT_DIR="${1:-dist/scan-wasm}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p "$OUT_DIR"

echo "building scan.wasm -> $OUT_DIR/scan.wasm"
GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o "$OUT_DIR/scan.wasm" ./cmd/scan-wasm

# wasm_exec.js ships with the Go toolchain; it MUST match the toolchain that
# built the binary. Go 1.24+ keeps it under lib/wasm; older releases under misc.
GOROOT="$(go env GOROOT)"
WASM_EXEC=""
for cand in "$GOROOT/lib/wasm/wasm_exec.js" "$GOROOT/misc/wasm/wasm_exec.js"; do
  if [ -f "$cand" ]; then WASM_EXEC="$cand"; break; fi
done
if [ -z "$WASM_EXEC" ]; then
  echo "error: wasm_exec.js not found under $GOROOT" >&2
  exit 1
fi
echo "copying wasm_exec.js ($WASM_EXEC) -> $OUT_DIR/wasm_exec.js"
cp "$WASM_EXEC" "$OUT_DIR/wasm_exec.js"

BYTES=$(wc -c < "$OUT_DIR/scan.wasm" | tr -d ' ')
echo "done: scan.wasm = $BYTES bytes (~$(( BYTES / 1024 / 1024 ))MB; ~1.3MB gzipped over the wire)"
