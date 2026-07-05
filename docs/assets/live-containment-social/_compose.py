#!/usr/bin/env python3
"""Compose per-beat frames: a terminal still on the house canvas plus its caption band.

Used by render.sh. Reads the caption bands and end card produced by _gen_captions.py and
the terminal frames extracted from the master GIF, and writes one composited PNG per beat.

The beat -> frame map exists because the committed cast was captured with a small idle
limit, so agg yields a handful of accumulating terminal states rather than smooth motion.
Each escape's BLOCKED line renders in the following state, so captions are aligned to the
frame that shows the denial, not the one that shows only the command. For the smooth final
cut, re-record with render.sh RECORD=1 (demo up) and the same bands overlay onto motion.
"""
import sys
from PIL import Image

BG = (11, 17, 36)

# beat index -> (terminal frame basename in workdir, caption band index)
# frame None means the generated end card.
BEATS = [
    ("f05", 0),   # setup: control plane up, sandbox engaged
    ("f26", 1),   # escape 1 exfil, BLOCKED visible
    ("f37", 2),   # escape 2 host read, BLOCKED visible
    ("f53", 3),   # escape 3 docker socket, BLOCKED visible
    ("f53", 4),   # verdict: containment summary
    (None, 5),    # end card CTA
]


def compose(work: str, banddir: str, w: int, h: int, term_w_frac: float,
            term_h_frac: float, term_y_frac: float, out_prefix: str) -> None:
    for i, (frame, band_idx) in enumerate(BEATS):
        out = f"{work}/{out_prefix}_{i:02d}.png"
        if frame is None:
            Image.open(f"{banddir}/endcard.png").convert("RGB").save(out)
            continue
        canvas = Image.new("RGB", (w, h), BG)
        term = Image.open(f"{work}/{frame}.png").convert("RGB")
        scale = min(int(w * term_w_frac) / term.width,
                    int(h * term_h_frac) / term.height)
        nw, nh = int(term.width * scale), int(term.height * scale)
        term = term.resize((nw, nh), Image.LANCZOS)
        canvas.paste(term, ((w - nw) // 2, int(h * term_y_frac)))
        band = Image.open(f"{banddir}/band_{band_idx:02d}.png").convert("RGBA")
        canvas.paste(band, (0, 0), band)
        canvas.save(out)


def main() -> None:
    # work banddir W H mode(landscape|square)
    work, banddir, w, h, mode = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
    if mode == "square":
        compose(work, banddir, w, h, 0.94, 0.62, 0.05, "sq_beat")
    else:
        compose(work, banddir, w, h, 0.86, 0.70, 0.06, "beat")


if __name__ == "__main__":
    main()
