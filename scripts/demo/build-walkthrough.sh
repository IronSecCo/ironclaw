#!/usr/bin/env bash
# build-walkthrough.sh — regenerate the end-to-end product walkthrough asset
# (docs/assets/walkthrough.{cast,svg}).
#
# Same pipeline as build.sh (the short hero demo), with one extra step: a
# prefers-reduced-motion guard is injected INTO the SVG so it travels with the
# asset everywhere it is embedded as <img> (README on GitHub + the landing).
#
# Pipeline:
#   1. gen-walkthrough.mjs → docs/assets/walkthrough.cast   (asciinema v2, 3-act
#                            faithful re-enactment of docs/quickstart.md).
#   2. svg-term-cli        → animated SVG (CSS filmstrip scroll, infinite loop).
#   3. brand remap         → recolor svg-term's 4 default-theme slots to steel-blue.
#   4. reduced-motion guard→ under prefers-reduced-motion:reduce the scroll freezes on
#                            the final frame (the approved-change audit trail), so the
#                            asset is motion-safe without a separate poster file.
#
# Requires: node + network (npx fetches svg-term-cli). Deterministic output.
set -euo pipefail
cd "$(dirname "$0")/../.."

CAST=docs/assets/walkthrough.cast
SVG=docs/assets/walkthrough.svg
COLS=94
ROWS=30

echo "[1/4] cast → $CAST"
node scripts/demo/gen-walkthrough.mjs > "$CAST"

echo "[2/4] svg-term → $SVG (raw)"
# `npm exec` (npx is rewritten by some local shells); pin the same version build.sh uses.
npm exec --yes -- svg-term-cli@2.1.1 --in "$CAST" --out "$SVG" --window --width "$COLS" --height "$ROWS"

echo "[3/4] brand remap (steel-blue)"
sed -i.bak \
  -e 's/#282d35/#0b1124/g' \
  -e 's/#b9c0cb/#eaf2ff/g' \
  -e 's/#a8cc8c/#5ad6a0/g' \
  -e 's/#6f7683/#63a0ff/g' \
  "$SVG"
rm -f "$SVG.bak"

echo "[4/4] inject prefers-reduced-motion guard (freeze on final frame)"
# svg-term scrolls a single <g> via a CSS keyframe `q`. We tag that group and add a
# media query that disables the animation and pins it to its final translateX, so
# reduced-motion users see a readable static frame (the audit trail) instead of motion.
node - "$SVG" <<'NODE'
const fs = require('fs');
const path = process.argv[2];
let svg = fs.readFileSync(path, 'utf8');

// final keyframe offset = the last translateX in @keyframes q
const kf = svg.match(/@keyframes q\{([\s\S]*?)\}\}/);
if (!kf) { console.error('  ! no @keyframes q found — svg-term output changed?'); process.exit(1); }
const xs = [...kf[1].matchAll(/translateX\((-?\d+(?:\.\d+)?)px\)/g)].map(m => m[1]);
const finalX = xs[xs.length - 1];

// tag the animated group (idempotent)
if (!svg.includes('class="iro-scroll"')) {
  svg = svg.replace('<g style="animation-duration:', '<g class="iro-scroll" style="animation-duration:');
}

// inject the guard once, just before </style>
const guard = `@media(prefers-reduced-motion:reduce){.iro-scroll{animation:none!important;transform:translateX(${finalX}px)!important}}`;
if (!svg.includes('prefers-reduced-motion')) {
  svg = svg.replace('</style>', guard + '</style>');
}

fs.writeFileSync(path, svg);
console.log('  reduced-motion guard pinned to translateX(' + finalX + 'px)');
NODE

echo "done → $SVG ($(wc -c < "$SVG") bytes)"
