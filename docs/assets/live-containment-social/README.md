# live-containment social cut (IRO-370)

A 60 to 90 second narrated, captioned screencast of a fully jailbroken agent trying to
break out of an IronClaw sandbox and being denied three times, cut for social with a star
plus install call to action. Landscape (16:9) and square (1:1) variants, captions burned in
so they read on muted autoplay.

- **[STORYBOARD.md](STORYBOARD.md)** is the script: the six beats, the exact on screen
  copy, the end card, and the design rationale. Board approved this copy (IRO-370).
- **captions.tsv** is the single source of truth for caption copy and timing. Everything
  else is generated from it, so the two variants and the sidecar `.srt` cannot drift.

The committed `landscape.mp4`, `square.mp4`, and `preview.gif` are the **final cut** (79s):
real terminal frames from the actual `examples/live-containment/run.sh` run, one beat per
escape, crossfaded, with captions burned in and the star plus install end card.

## Files

| File | What it is |
|------|------------|
| `landscape.mp4`, `square.mp4`, `preview.gif` | the final cut, 16:9 and 1:1 plus a light loop. |
| `captions.srt`    | sidecar caption track for platforms that accept uploads. |
| `STORYBOARD.md`   | the six beat script and rationale. |
| `captions.tsv`    | caption copy and timing, single source of truth. |
| `render.sh`       | one command rebuild of both variants plus preview and srt. |
| `_gen_captions.py`| rasterizes caption bands and the end card, emits `captions.srt`. |
| `_compose.py`     | composes each beat: terminal frame on the house canvas plus its band. |

## Rebuild

```bash
docs/assets/live-containment-social/render.sh            # writes landscape.mp4, square.mp4, preview.gif, captions.srt
XF=0.9 docs/assets/live-containment-social/render.sh     # longer crossfade
DRAFT=1 docs/assets/live-containment-social/render.sh    # writes *-draft.* copies instead of overwriting finals
```

Deps: `agg`, `ffmpeg`, and a `python3` with Pillow. Text is rasterized with Pillow and
composited with ffmpeg `overlay` and `xfade`, so no libass or drawtext build of ffmpeg is
required. Caption copy is dash checked at generation time (house style, IRO-254).

## Why a paced animatic, not a screen recording

The demo prints its whole output near-instantly (`run.sh` echoes blocks, it does not type),
so a raw recording is a sub-second dump, not a 60 to 90 second narrated piece. Confirmed by
recording a fresh `--attach` run: every event lands at about t=0. The deliberate per beat
pacing here is the narration, and the crossfades give motion between beats. This is the
intended form for the cut, not a stopgap.

## Residual

The `run.sh` terminal source copy (banner and BLOCKED lines) still contains em dashes. Our
burned captions and end card are dash free, but the terminal text in frame is not. Making
the whole frame house-clean is a separate `examples/live-containment/run.sh` copy change,
coordinated so the demo assertion still passes.
