# Channel triage bot (Slack)

A triage assistant that sits in a shared Slack channel and is deliberately
**quiet**: it engages only when `@mentioned`, only acts for **known** senders,
and **accumulates** the messages it doesn't engage on so it has context when it
is finally called in.

## What it configures

- An agent group `triage`.
- A Slack **channel** messaging group with a `strict` unknown-sender policy
  (messages from unregistered senders are not acted on).
- A known on-call user, added as a **member** of the group.
- A wiring tuned for triage:
  - `--engage mention` — only responds to an `@mention`.
  - `--scope known` — only for registered users.
  - `--ignored accumulate` — keeps non-engaging messages as context.
  - `--session shared` — one shared thread of context for the channel.
- A delivery destination so it can post back into the channel.

## Try it

```sh
cd examples/channel-triage
./setup.sh
```

Edit the placeholders at the top of [`setup.sh`](setup.sh): the Slack channel id
(`C…`) and the on-call user id (`slack:U…`).

## What to notice

- Because the policy is `strict` and the scope is `known`, a stranger in the
  channel cannot make the bot act — a useful default for a shared workspace.
- `--ignored accumulate` means the bot isn't blind to the conversation it stayed
  out of: when mentioned, it has the recent context.
