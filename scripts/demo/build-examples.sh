#!/usr/bin/env bash
# build-examples.sh — regenerate the animated per-example demo assets
# (docs/assets/hello.{cast,svg} and docs/assets/redteam.{cast,svg}).
#
# These are the motion assets for the top two gallery examples (IRO-291): a short
# terminal recording converts visitors who won't read a wall of text. Same pipeline,
# honesty, and reduced-motion guard as the hero demo (scripts/demo/build.sh, IRO-100/191):
#
#   1. gen-<name>-cast.mjs → docs/assets/<name>.cast   (deterministic asciinema v2,
#                                                        faithful re-enactment of the
#                                                        example's run.sh).
#   2. svg-term-cli        → an animated SVG (per-character typing, infinite loop).
#   3. brand remap         → recolor svg-term's 4 default-theme slots to the IronClaw
#                            steel-blue palette (svg-term@2.1.1 ignores --profile/theme).
#   4. a11y guard          → inject a prefers-reduced-motion rule that freezes the loop
#                            on its final (completed) frame. The guard travels INSIDE the
#                            SVG, so it is honored everywhere the <img> is embedded
#                            (GitHub README + docs site) with no separate poster file.
#
# Requires: node + network (npx fetches svg-term-cli). Deterministic output.
set -euo pipefail
cd "$(dirname "$0")/../.."

# name  cast->svg  gen-script  width  height
build_one() {
  local name="$1" width="$2" height="$3"
  local cast="docs/assets/${name}.cast"
  local svg="docs/assets/${name}.svg"
  local gen="scripts/demo/gen-${name}-cast.mjs"

  echo "== ${name} =="
  echo "[1/4] cast → $cast"
  node "$gen" > "$cast"

  echo "[2/4] svg-term → $svg (raw)"
  npx --yes svg-term-cli@2.1.1 --in "$cast" --out "$svg" --window --width "$width" --height "$height"

  echo "[3/4] brand remap (steel-blue)"
  sed -i.bak \
    -e 's/#282d35/#0b1124/g' \
    -e 's/#b9c0cb/#eaf2ff/g' \
    -e 's/#a8cc8c/#5ad6a0/g' \
    -e 's/#6f7683/#63a0ff/g' \
    "$svg"
  rm -f "$svg.bak"

  echo "[4/4] a11y reduced-motion guard"
  python3 - "$svg" <<'PY'
import re, sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
# svg-term names the infinite scroll animation with an auto-incrementing single
# letter (n, o, …) that varies per asset, so match the animation block structurally
# (the steps(1,end) scroll <g>) rather than hardcoding the letter.
s, n1 = re.subn(r'<g style="(animation-duration:[^"]*animation-name:[a-z]+[^"]*)"',
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

  echo "done → $svg ($(wc -c < "$svg") bytes)"
  echo
}

build_one hello   92 24
build_one redteam 92 26
