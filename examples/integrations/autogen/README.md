# AutoGen agents, sandboxed by IronClaw

Your **AutoGen** (Microsoft) agent runs untrusted, model-generated code. Point
that execution at an **IronClaw sandbox** instead of your host and the same agent
gets real code execution with **no network, no host filesystem, and no Docker
socket** — the isolation boundary IronClaw [proves holds](../../red-team-escape/),
not just promises.

```python
from ironclaw_sandbox import IronClawSandbox
from ironclaw_tool import make_sandbox_tool

with IronClawSandbox() as sandbox:            # engage a per-session sandbox
    tool = make_sandbox_tool(sandbox)         # a real AutoGen FunctionTool
    # agent = AssistantAgent("coder", model_client=..., tools=[tool])
```

The `sandboxed_shell` FunctionTool is a drop-in for the host-executing shell /
code tool you would otherwise hand an `AssistantAgent`. The agent plans the tool
calls exactly as before; only the execution moves into the box.

## Try it in one command

Zero credentials — the LLM side uses the offline **mock provider**, the sandbox
is real:

```sh
examples/integrations/autogen/run.sh
```

It engages a live IronClaw per-session sandbox, drives the `sandboxed_shell`
FunctionTool exactly as an `AssistantAgent`'s tool loop would
(`tool.run_json({"command": ...})`), runs one benign task plus a battery of escape
attempts, and prints a PASS/FAIL containment table:

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

Set `OPENAI_API_KEY` (and uncomment `autogen-agentchat` + `autogen-ext[openai]` in
[`requirements.txt`](requirements.txt)) and `run.sh` drives a real
`AssistantAgent` instead of the scripted probes. The tool — and therefore the
isolation — is identical.

## How it works

- [`ironclaw_sandbox.py`](../_shared/ironclaw_sandbox.py) — engages a per-session
  IronClaw sandbox (`ic-sbx-*`) against the demo control-plane and runs commands
  inside it as its own non-root uid (65532). Pure standard library.
- [`ironclaw_tool.py`](ironclaw_tool.py) — wraps that sandbox as an AutoGen
  `FunctionTool` named `sandboxed_shell`. **This is the ~15 lines you copy** to
  swap a host shell tool for a sandboxed one.
- [`run.py`](run.py) — engages the sandbox and drives the tool.

The execution primitive is the same one IronClaw's red-team harness attacks: a
`docker exec` into the live per-session sandbox as its non-root uid. See the
repo root [`README`](../../../README.md) for the full isolation model.
