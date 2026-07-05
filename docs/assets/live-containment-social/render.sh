#!/usr/bin/env bash
# render.sh — build the IRO-370 narrated, captioned live-containment social cut.
#
# Outputs, in this directory:
#   landscape.mp4   16:9, captions burned in, end card appended
#   square.mp4      1:1,  captions burned in, end card appended
#   preview.gif     lightweight loop for inline embeds
#   captions.srt    sidecar caption track (uploadable where platforms accept it)
#
# Caption copy and timing come from captions.tsv, the single source of truth. Text is
# rasterized with Pillow (_gen_captions.py) and composited with ffmpeg's basic overlay,
# so this works with an ffmpeg built without libass or drawtext.
#
# House style: no em or en dashes in on screen copy (IRO-254); the generator rejects them.
# Run this after the board approves STORYBOARD.md.
#
# Deps: agg, ffmpeg, and a python3 with Pillow (PIL). asciinema only when RECORD=1.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
CAST="${CAST:-$REPO_ROOT/docs/assets/live-containment.cast}"
WORK="$HERE/.work"
DRAFT="${DRAFT:-0}"                       # DRAFT=1 writes *-draft.mp4 review copies
SUFFIX=""; [[ "$DRAFT" == "1" ]] && SUFFIX="-draft"
mkdir -p "$WORK" "$WORK/sq"

# A python3 that has Pillow. The repo default python (pyenv) has it; a bare
# /usr/local/bin/python3 may not, so resolve one that imports PIL.
PY=""
for c in python3 /opt/homebrew/bin/python3 /usr/bin/python3; do
  if command -v "$c" >/dev/null 2>&1 && "$c" -c "import PIL" 2>/dev/null; then PY="$c"; break; fi
done
[[ -n "$PY" ]] || { echo "no python3 with Pillow found (pip install pillow)"; exit 1; }

THEME="0b1124,eaf2ff,1d2c57,ff8087,5ad6a0,e3b341,3b82f6,b9a0ff,63a0ff,b9d4ff,3a4a7a,ff9aa0,7ce8bb,f0c460,63a0ff,cdbcff,93b6ff,ffffff"
DUR=(12 16 16 16 12 10)                   # per-beat seconds, matches captions.tsv (82s)

# 0. Optional fresh, naturally paced recording (smooth motion final).
if [[ "${RECORD:-0}" == "1" ]]; then
  echo "==> recording fresh run into $CAST (demo must be up: run.sh --keep)"
  asciinema rec "$CAST" --overwrite --idle-time-limit 3.0 --window-size 92x28 \
    --command "SKIP_BUILD=1 $REPO_ROOT/examples/live-containment/run.sh --attach"
fi
[[ -f "$CAST" ]] || { echo "cast not found: $CAST (set RECORD=1 to capture)"; exit 1; }

# 1. Cast -> master GIF -> terminal frames.
echo "==> agg: cast -> master.gif -> frames"
agg --font-size 16 --fps-cap 12 --speed 1.0 --idle-time-limit 3.0 \
  --last-frame-duration 3 --theme "$THEME" "$CAST" "$WORK/master.gif"
rm -f "$WORK"/f*.png
ffmpeg -y -i "$WORK/master.gif" "$WORK/f%02d.png" >/dev/null 2>&1

# 2. Caption bands + end card + sidecar srt, per aspect. srt lives next to the assets.
echo "==> caption assets from captions.tsv"
"$PY" "$HERE/_gen_captions.py" "$HERE/captions.tsv" "$WORK" 1920 1080
"$PY" "$HERE/_gen_captions.py" "$HERE/captions.tsv" "$WORK/sq" 1080 1080
cp "$WORK/captions.srt" "$HERE/captions.srt"

# 3. Compose per-beat frames.
echo "==> composing beats"
"$PY" "$HERE/_compose.py" "$WORK" "$WORK"    1920 1080 landscape
"$PY" "$HERE/_compose.py" "$WORK" "$WORK/sq" 1080 1080 square

# 4. Encode each variant: per-beat clip at its duration, then concat (exact runtime).
encode () { # prefix out
  local prefix=$1 out=$2
  local list="$WORK/list_${prefix}.txt"; : > "$list"
  for i in 0 1 2 3 4 5; do
    ffmpeg -y -loop 1 -i "$WORK/${prefix}_0${i}.png" -t "${DUR[$i]}" -r 12 \
      -vf format=yuv420p -c:v libx264 -crf 23 "$WORK/${prefix}_clip_${i}.mp4" >/dev/null 2>&1
    printf "file '%s/${prefix}_clip_%d.mp4'\n" "$WORK" "$i" >> "$list"
  done
  ffmpeg -y -f concat -safe 0 -i "$list" -c copy "$out" >/dev/null 2>&1
}
echo "==> encoding landscape + square"
encode beat   "$HERE/landscape${SUFFIX}.mp4"
encode sq_beat "$HERE/square${SUFFIX}.mp4"

# 5. Lightweight preview gif from the landscape cut.
echo "==> preview.gif"
ffmpeg -y -i "$HERE/landscape${SUFFIX}.mp4" -vf \
  "fps=8,scale=760:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  "$HERE/preview${SUFFIX}.gif" >/dev/null 2>&1

echo "==> done:"; ls -lh "$HERE"/landscape${SUFFIX}.mp4 "$HERE"/square${SUFFIX}.mp4 "$HERE"/preview${SUFFIX}.gif
echo "confirm each MP4 is under 30MB before hand off."
