# Letta (MemGPT) agents, sandboxed by IronClaw

Your **Letta** (formerly MemGPT) agent lets the model call tools -- and a code /
shell tool runs untrusted, model-chosen commands. Point that execution at an
**IronClaw sandbox** instead of your host and the same agent gets real code
execution with **no network, no host filesystem, and no Docker socket** -- the
isolation boundary IronClaw [proves holds](../../red-team-escape/), not just
promises.

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool

with IronClawSandbox() as sandbox:            # engage a per-session sandbox
    tool = make_sandbox_tool(sandbox)         # a Letta client-side tool
    # client.agents.messages.create(agent_id=..., messages=[...],
    #                               client_tools=[tool.schema])
```

Letta runs a tool's Python source inside its own server-side tool sandbox by
default, so a closure over a live handle can't be registered that way. Letta's
**client-side tool execution** is the honest fit: the agent gets the tool
*schema*, and when the model calls it your process runs the command -- here,
inside the IronClaw sandbox -- and returns the result. `make_sandbox_tool` gives
you both faces: `tool.schema` for the `client_tools` parameter and
`tool(command=...)` as the executor.

## Try it in one command

Zero credentials -- the LLM side uses the offline **mock provider**, the sandbox
is real, and nothing needs installing (the client-side tool is pure standard
library):

```sh
examples/integrations/letta/run.sh
```

It engages a live IronClaw per-session sandbox, dispatches the `sandboxed_shell`
tool exactly as a Letta client-side tool call would (`tool(command=...)`), runs
one benign task plus a battery of escape attempts, and prints a PASS/FAIL
containment table:

```
  [OK ] benign task: run agent code                    ->  [exit 0] hello from inside the IronClaw sandbox uid=65532...
  [OK ] network egress: only loopback exists           ->  [exit 0] lo
  [OK ] network egress: DNS lookup of api.anthropic...  ->  [exit 0] NO_EGRESS
  [OK ] host escape: Docker Engine socket is absent    ->  [exit 0] ABSENT
  [OK ] host escape: host filesystem is not mounted    ->  [exit 0] CONTAINED

RESULT: PASS -- benign code ran; every escape attempt was contained.
```

`run.sh --keep` leaves the demo running; `run.sh --attach` reuses an already-up
demo control-plane.

## Use a real LLM

Run a Letta server, set `LETTA_BASE_URL` (for example `http://localhost:8283`),
and `run.sh` drives a real Letta agent instead of the scripted probes. The agent
plans the tool calls; your client executes each one against the sandbox and posts
the result back (`pip install letta-client` first). The executor -- and therefore
the isolation -- is identical.

## How it works

- [`ironclaw_sandbox.py`](../_shared/ironclaw_sandbox.py) -- engages a per-session
  IronClaw sandbox (`ic-sbx-*`) against the demo control-plane and runs commands
  inside it as its own non-root uid (65532). Pure standard library.
- [`ironclaw_tool.py`](ironclaw_tool.py) -- wraps that sandbox as a Letta
  client-side tool named `sandboxed_shell`. **This is the ~20 lines you copy** to
  swap a host shell tool for a sandboxed one.
- [`run.py`](run.py) -- engages the sandbox and drives the tool.

The execution primitive is the same one IronClaw's red-team harness attacks: a
`docker exec` into the live per-session sandbox as its non-root uid. See the
repo root [`README`](../../../README.md) for the full isolation model.
