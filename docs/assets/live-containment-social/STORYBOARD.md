# live-containment social cut, storyboard (IRO-370)

A 60 to 90 second narrated, captioned screencast of a fully jailbroken agent trying to
break out of an IronClaw sandbox and being denied at the isolation boundary, three times,
ending on a star plus install call to action.

This is the board-review draft. Nothing renders as final until the board approves the
copy below. Source footage is the real `examples/live-containment/run.sh` recording
(`docs/assets/live-containment.cast`), the same script behind the existing hero clip, so
the terminal content is a genuine run, not staged text.

- Deliverables on approval: `landscape.mp4` (16:9), `square.mp4` (1:1), `preview.gif`
  (lightweight loop). All under this directory. MP4s target under 30 MB.
- Captions are burned in and readable with sound off (autoplay muted friendly).
- House style: no em or en dashes in any on screen copy (IRO-254).
- Single source of truth for caption copy and timing: `captions.tsv`. `render.sh`
  generates the landscape and square subtitle tracks from it, so the two variants can
  never drift.

## Target running time

Six beats, roughly 82 seconds, inside the 60 to 90 second window.

| # | Beat            | In    | Out   | On screen (terminal)                                  | Caption (burned in)                                                          |
|---|-----------------|-------|-------|-------------------------------------------------------|-----------------------------------------------------------------------------|
| 1 | Setup           | 0:00  | 0:12  | Control plane boots, a live per session sandbox engages, banner names the box under test | A jailbroken agent runs code as root inside the sandbox. IronClaw assumes it already won. |
| 2 | Escape 1, exfil | 0:12  | 0:28  | `getent hosts api.anthropic.com` then BLOCKED         | Escape 1, phone home to exfiltrate. network=none, only loopback exists. Denied. |
| 3 | Escape 2, host  | 0:28  | 0:44  | `cat /host/etc/shadow` then BLOCKED                    | Escape 2, read the operator host. The host root is outside the sandbox namespace. Denied. |
| 4 | Escape 3, socket| 0:44  | 1:00  | `docker -H unix:///var/run/docker.sock run` then BLOCKED | Escape 3, seize the Docker Engine socket. It was never mounted in. Denied.  |
| 5 | Verdict         | 1:00  | 1:12  | CONTAINMENT SUMMARY, 3 of 3 DENIED                    | 3 of 3 escapes denied. The box held. Isolation you can prove, not just promise. |
| 6 | End card, CTA   | 1:12  | 1:22  | Full frame end card (generated), not terminal         | Star IronClaw on GitHub. brew install ironsecco/ironclaw/ironclaw            |

## Why this order reads (design rationale)

- Serial position and Peak End: the strongest, most concrete threat (host takeover via the
  Docker socket) sits at beat 4, right before the verdict, so the peak lands late and the
  end card is the last thing retained.
- Chunking and Miller: exactly three escapes, each a single caption line under three
  seconds to read. No viewer has to hold more than one claim at a time.
- Recognition over recall: every caption restates the exact mechanism the terminal shows
  on the same frame (`network=none`, mount namespace, unmounted socket), so the words and
  the proof are co located (Common Region, proximity).
- Trust signals: captions name the real control (not marketing verbs). The line
  "Isolation you can prove, not just promise" is the emotional beat for a security buyer,
  earned only after three concrete denials.
- Fitts and CTA: the end card is a full frame, high contrast, two lines only, one action
  (star) plus one command (install). No competing targets.

## End card copy (beat 6)

```
Isolation you can prove.

github.com/IronSecCo/ironclaw          (star the repo)
brew install ironsecco/ironclaw/ironclaw
```

- Star line is the primary action, install is the secondary, one line each.
- No dashes. High contrast on the dark terminal palette used across our assets
  (`#0b1124` background, `#eaf2ff` text, `#5ad6a0` accent for the pass state).

## Accessibility notes

- Captions are burned in, so the message survives on muted autoplay and on platforms that
  strip caption tracks. A sidecar `captions.srt` is also emitted for platforms that accept
  uploaded captions, so screen reader and non visual users are not excluded (WCAG 1.2).
- Color is never the only signal: every denial pairs the green or red state with the word
  BLOCKED and Denied, so it reads for color blind viewers (WCAG 1.4.1).
- Caption band uses a solid backing plate for contrast against moving terminal text
  (WCAG 1.4.3), not text directly over the terminal.
- Pacing holds each escape frame for at least four seconds so the caption is readable at a
  comfortable reading level; nothing flashes (no motion trigger risk).

## Production pipeline

`render.sh` in this directory is the one command render, run after board approval:

1. Record a fresh, naturally paced run of `examples/live-containment/run.sh` with
   `asciinema` (the built in sleeps between escapes give the 60 to 90 second pacing), or
   reuse the committed `docs/assets/live-containment.cast`.
2. `agg` renders the cast to a master GIF at true speed with pacing preserved.
3. `ffmpeg` pads to 1920x1080, burns the landscape caption track, appends the generated
   end card, writes `landscape.mp4`.
4. `ffmpeg` reframes to 1080x1080, burns the square caption track, appends the end card,
   writes `square.mp4`.
5. `ffmpeg` down samples `landscape.mp4` to a palette optimized `preview.gif` for the
   lightweight loop.

Caption copy and timings are read from `captions.tsv` so the storyboard, the two burned in
variants, and the sidecar `.srt` are always the same words.

## Open questions for the board

1. CTA wording: "Star IronClaw on GitHub" plus the brew line, or swap brew for the
   `curl | sh` one liner for the non Homebrew audience?
2. Repo URL vs a short vanity link on the end card. Current draft uses the canonical
   `github.com/IronSecCo/ironclaw`.
3. Should we add a one word audio VO later, or keep it caption only (current plan is
   caption only, which is the safer default for muted social autoplay)?
