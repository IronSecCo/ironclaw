# Keyword watcher (Discord)

An ops-channel agent that stays silent until a message matches a **pattern** —
then engages. It watches a busy Discord channel for words like `deploy`,
`incident`, `outage`, or `rollback`, and only those messages wake it. Useful for
an alerts/on-call channel where you want help exactly when something is on fire,
not on every line of chatter.

## What it configures

- An agent group `watcher`.
- A Discord **channel** messaging group with a `public` policy (anyone in the
  channel can trip the pattern — appropriate for an open ops channel).
- A wiring tuned for keyword watching:
  - `--engage pattern --pattern '(?i)\b(deploy|incident|outage|rollback)\b'` —
    engages only when the message matches the (case-insensitive) regex.
  - `--scope all` — any sender can trip it (not just registered users).
  - `--session per-thread` — each incident gets its own conversation context.
- A delivery destination so it can reply back into the channel.

## Try it

```sh
cd examples/keyword-watcher
./setup.sh
```

Edit the placeholders at the top of [`setup.sh`](setup.sh): the Discord channel
id and (optionally) the keyword pattern.

> Discord is one of the adapters that **auto-registers from the environment** —
> set `DISCORD_BOT_TOKEN` in the control-plane's environment so it can deliver.
> See [`docs/channels.md`](../../docs/channels.md) for every adapter's credentials.

## What to notice

- `--engage pattern` is the third engaging style alongside `mention`
  ([`channel-triage/`](../channel-triage/)) and reply-to-everything
  (`--pattern '.*'`, [`personal-assistant/`](../personal-assistant/)). The regex
  is matched against each inbound message; non-matching chatter is ignored.
- `--scope all` + `--policy public` means it helps anyone — the right call for an
  open incident channel, the opposite of `channel-triage/`'s `strict` + `known`.
- `--session per-thread` keeps two simultaneous incidents from blurring into one
  context.
