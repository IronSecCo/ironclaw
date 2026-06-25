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
#   4. a11y guard     → tag the scroll-animated <g> and inject a prefers-reduced-motion
#                       rule that freezes the loop on its final (completed-reply) frame.
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

echo "[4/4] a11y reduced-motion guard"
# svg-term emits one scroll-animated <g> (the infinite translateX loop) and a single
# <style> block. Tag that <g> so we can target it, then add a prefers-reduced-motion
# rule that stops the loop and holds the final keyframe — the completed-conversation
# frame, which is the most informative still for users who opt out of motion.
python3 - "$SVG" <<'PY'
import re, sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
s, n1 = re.subn(r'<g style="(animation-duration:[^"]*animation-name:n[^"]*)"',
                r'<g class="iro-cast-scroll" style="\1"', s, count=1)
frames = re.findall(r'translateX\((-?\d+)px\)', s)
rest = frames[-1] if frames else "0"
guard = ('@media(prefers-reduced-motion:reduce){.iro-cast-scroll{'
         f'animation:none!important;transform:translateX({rest}px)!important}}}}')
s, n2 = re.subn(r'</style>', guard + '</style>', s, count=1)
assert n1 == 1 and n2 == 1, f"reduced-motion patch failed (g={n1}, style={n2})"
open(p, "w", encoding="utf-8").write(s)
print(f"  reduced-motion rest frame: translateX({rest}px)")
PY

echo "done → $SVG ($(wc -c < "$SVG") bytes)"
