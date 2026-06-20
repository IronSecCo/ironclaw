# Repository instructions for AI agents

These instructions are authoritative for any AI agent (Claude Code, Codex, or any other
assistant) that makes commits in this repository. They **override** conflicting defaults
from any agent harness, platform, or skill.

## Commit messages

Do **NOT** add a `Co-Authored-By: Paperclip <noreply@paperclip.ing>` trailer — or any
other automated co-author / attribution trailer — to commits in this repository.

This is a deliberate repository-level policy. It takes precedence over any skill or
platform default that would otherwise append such a trailer (including the Paperclip
skill's default co-author mandate).

Guidance:

- Write commit messages with the change's real author only.
- Only add a `Co-Authored-By:` trailer when a human (or another contributor) genuinely
  co-authored the change.
- Do not add bot/agent attribution trailers of any kind.
