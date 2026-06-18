# Personal assistant (Telegram DM)

A private, 1:1 assistant: one agent group wired to your Telegram direct messages
so it replies to **every** message you send it, with per-conversation session
state. This template also walks through the **mandatory change-approval flow** —
the heart of IronClaw's security model.

## What it configures

- An agent group `assistant`.
- You, as the **owner** of that group (a Telegram identity).
- A Telegram DM messaging group (not a group chat).
- A wiring that engages on every message (`--engage pattern --pattern '.*'`),
  scoped to known senders, with one session per thread.
- A delivery destination so the agent may reply back to your DM.

Then it demonstrates that **no capability change applies without a human
decision**: it submits a `persona` change, shows it held as pending, and
approves it.

## Try it

```sh
# from the repo root, with a control-plane running and IRONCLAW_API_TOKEN set
cd examples/personal-assistant
./setup.sh
```

Edit the placeholders at the top of [`setup.sh`](setup.sh) first — your Telegram
numeric user id is the recipient/`platform` id.

## What to notice

- The wiring uses `--scope known`, so the assistant ignores anyone who is not a
  registered user of the group — a stranger who finds your bot gets nothing.
- The change you submit is **HELD** at the gateway (`change pending`) until you
  approve it. There is no path that mutates the agent without that step; the CLI
  carries the change envelope (kind/group/requester) and the control-plane
  records the decision in the audit log (`ironctl audit`).
