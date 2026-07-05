# live-containment social cut (IRO-370)

A 60 to 90 second narrated, captioned screencast of a fully jailbroken agent trying to
break out of an IronClaw sandbox and being denied three times, cut for social with a star
plus install call to action. Landscape (16:9) and square (1:1) variants, captions burned in
so they read on muted autoplay.

- **[STORYBOARD.md](STORYBOARD.md)** is the board-review script: the six beats, the exact
  on screen copy, the end card, and the design rationale. Review copy there before the
  final render.
- **captions.tsv** is the single source of truth for caption copy and timing. Everything
  else is generated from it, so the two variants and the sidecar `.srt` cannot drift.

## Files

| File | What it is |
|------|------------|
| `STORYBOARD.md`   | The six beat script and rationale, for board review. |
| `captions.tsv`    | Caption copy and timing, single source of truth. |
| `render.sh`       | One command build of both variants plus preview and srt. |
| `_gen_captions.py`| Rasterizes caption bands and the end card, emits `captions.srt`. |
| `_compose.py`     | Composes each beat: terminal still on the house canvas plus its band. |
| `*-draft.mp4/gif` | Draft cut for board review (built with `DRAFT=1`). |

## Build

```bash
docs/assets/live-containment-social/render.sh            # writes landscape.mp4, square.mp4, preview.gif, captions.srt
DRAFT=1 docs/assets/live-containment-social/render.sh    # writes *-draft.mp4 review copies
RECORD=1 docs/assets/live-containment-social/render.sh   # re-record the run first (demo must be up), smooth motion
```

Deps: `agg`, `ffmpeg`, and a `python3` with Pillow. `asciinema` only for `RECORD=1`.
Text is rasterized with Pillow and composited with ffmpeg `overlay`, so no libass or
drawtext build of ffmpeg is required. Caption copy is dash checked at generation time
(house style, IRO-254).

## Status and residuals

The committed `*-draft.*` files are an **animatic**: real terminal frames from the actual
`examples/live-containment/run.sh` recording, held per beat with captions and the end card.
It exists to review copy, timing, and framing. Two things before the final render:

1. **Smooth motion.** The committed cast was captured with a small idle limit, so it
   collapses to a few accumulating terminal states. For the final, re-record at natural
   pace (`RECORD=1`, demo up with `run.sh --keep`); the same caption bands overlay onto the
   motion cut.
2. **Terminal source copy uses em dashes.** The run.sh banner and BLOCKED lines contain
   `—`. Our burned captions and end card are dash free, but the terminal text in frame is
   not. If we want the whole frame house-clean, adjust the copy in
   `examples/live-containment/run.sh` before recording (separate change, coordinate so the
   demo assertion still passes).
