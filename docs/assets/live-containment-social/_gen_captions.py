#!/usr/bin/env python3
"""Generate burned-in caption assets from captions.tsv (the single source of truth).

captions.tsv rows: start<TAB>end<TAB>text   (\\n inside text = line break)

Emits, into OUTDIR:
  band_NN.png        one transparent 1-per-beat caption plate (band + text)
  endcard.png        full-frame end card (star CTA + install one liner)
  captions.srt       sidecar caption track for platforms that accept uploads
  timing.env         START/END/DUR arrays sourced by render.sh

Portable path: text is rasterized here with Pillow so ffmpeg only needs the basic
`overlay` filter (this repo's ffmpeg is built without libass/drawtext). The caption plate
is a solid backing band anchored to the lower third for contrast against moving terminal
text (WCAG 1.4.3). House style forbids em and en dashes; bad copy is rejected, not rendered.

Usage: _gen_captions.py captions.tsv OUTDIR W H
"""
import os
import sys
from PIL import Image, ImageDraw, ImageFont

BG = (11, 17, 36)        # 0b1124 terminal background
FG = (234, 242, 255)     # eaf2ff foreground
PASS = (90, 214, 160)    # 5ad6a0 pass/accent
BAND = (11, 17, 36, 210)  # semi-opaque plate

FONT_REG = "/System/Library/Fonts/Supplemental/Arial.ttf"
FONT_BOLD = "/System/Library/Fonts/Supplemental/Arial Bold.ttf"
FONT_MONO = "/System/Library/Fonts/Menlo.ttc"


def load(path: str, size: int) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(path, size)


def parse(tsv_path: str):
    rows = []
    with open(tsv_path, encoding="utf-8") as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            start, end, text = line.split("\t", 2)
            if "—" in text or "–" in text:
                raise SystemExit(f"em/en dash in caption (IRO-254): {text!r}")
            rows.append((float(start), float(end), text.replace("\\n", "\n")))
    return rows


def draw_center(draw, cx, y, text, font, fill):
    box = draw.textbbox((0, 0), text, font=font)
    w = box[2] - box[0]
    draw.text((cx - w / 2, y), text, font=font, fill=fill)
    return box[3] - box[1]


def band_png(text: str, w: int, h: int, out: str) -> None:
    img = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    fs = max(30, h // 22)
    font = load(FONT_BOLD, fs)
    lines = text.split("\n")
    line_h = int(fs * 1.35)
    block_h = line_h * len(lines)
    pad = int(fs * 0.8)
    band_top = h - block_h - pad * 2 - h // 16
    d.rectangle([0, band_top, w, band_top + block_h + pad * 2], fill=BAND)
    y = band_top + pad
    for ln in lines:
        draw_center(d, w / 2, y, ln, font, FG)
        y += line_h
    img.save(out)


def endcard_png(w: int, h: int, out: str) -> None:
    img = Image.new("RGBA", (w, h), BG + (255,))
    d = ImageDraw.Draw(img)
    title = load(FONT_BOLD, max(44, h // 12))
    url = load(FONT_BOLD, max(30, h // 22))
    mono = load(FONT_MONO, max(26, h // 26))
    draw_center(d, w / 2, h * 0.28, "Isolation you can prove.", title, FG)
    draw_center(d, w / 2, h * 0.50, "github.com/IronSecCo/ironclaw", url, PASS)
    draw_center(d, w / 2, h * 0.58, "star the repo", load(FONT_REG, max(20, h // 40)), FG)
    draw_center(d, w / 2, h * 0.68, "brew install ironsecco/ironclaw/ironclaw", mono, FG)
    img.convert("RGB").save(out)


def ts_srt(t: float) -> str:
    h = int(t // 3600)
    m = int((t % 3600) // 60)
    s = int(t % 60)
    ms = int(round((t - int(t)) * 1000))
    return f"{h:02d}:{m:02d}:{s:02d},{ms:03d}"


def main() -> None:
    tsv, outdir, w, h = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
    rows = parse(tsv)
    os.makedirs(outdir, exist_ok=True)
    srt, starts, ends = [], [], []
    for i, (start, end, text) in enumerate(rows):
        band_png(text, w, h, os.path.join(outdir, f"band_{i:02d}.png"))
        srt.append(f"{i+1}\n{ts_srt(start)} --> {ts_srt(end)}\n{text}\n")
        starts.append(str(start))
        ends.append(str(end))
    endcard_png(w, h, os.path.join(outdir, "endcard.png"))
    with open(os.path.join(outdir, "captions.srt"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(srt))
    with open(os.path.join(outdir, "timing.env"), "w", encoding="utf-8") as fh:
        fh.write(f"NBEATS={len(rows)}\n")
        fh.write(f"STARTS=({' '.join(starts)})\n")
        fh.write(f"ENDS=({' '.join(ends)})\n")


if __name__ == "__main__":
    main()
