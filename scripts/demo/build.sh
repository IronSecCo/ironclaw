#!/usr/bin/env bash
# build.sh — regenerate the IronClaw hero demo asset (docs/assets/demo.{cast,svg}).
#
# Pipeline:
#   1. gen-cast.mjs   → docs/assets/demo.cast   (asciinema v2, faithful re-enactment
#                                                 of the validated zero-credential chat
#                                                 demo; outputs captured from a live
#                                                 docker-compose.demo.yml run).
#   2. svg-term-cli   → an animated SVG (per-character typing, infinite loop).
#   3. brand remap    → recolor svg-term's 4 default-theme slots to the IronClaw
#                       steel-blue palette. svg-term-cli@2.1.1 ignores --profile and the
#                       cast `theme`, so we patch its emitted colors deterministically.
#
# Requires: node + network (npx fetches svg-term-cli). Deterministic output.
set -euo pipefail
cd "$(dirname "$0")/../.."

CAST=docs/assets/demo.cast
SVG=docs/assets/demo.svg

echo "[1/3] cast → $CAST"
node scripts/demo/gen-cast.mjs > "$CAST"

echo "[2/3] svg-term → $SVG (raw)"
npx --yes svg-term-cli@2.1.1 --in "$CAST" --out "$SVG" --window --width 92 --height 26

echo "[3/3] brand remap (steel-blue)"
# svg-term default-theme slot  →  IronClaw brand color
#   #282d35 window/terminal bg →  #0b1124 deep navy
#   #b9c0cb foreground (+bold)  →  #eaf2ff near-white blue
#   #a8cc8c green (prompt/✔)    →  #5ad6a0 brand green
#   #6f7683 block cursor        →  #63a0ff steel-blue cursor
# (256-color spans 67/110/153 and the window traffic-lights are already on-brand.)
sed -i.bak \
  -e 's/#282d35/#0b1124/g' \
  -e 's/#b9c0cb/#eaf2ff/g' \
  -e 's/#a8cc8c/#5ad6a0/g' \
  -e 's/#6f7683/#63a0ff/g' \
  "$SVG"
rm -f "$SVG.bak"

echo "done → $SVG ($(wc -c < "$SVG") bytes)"
