# LlamaIndex Tool: IronClaw sandbox

`llama-index-tools-ironclaw` lets a LlamaIndex agent run untrusted,
model-generated code inside an **IronClaw sandbox** instead of on your host. The
agent calls it exactly like a normal shell/code tool, but every command runs
with **no network, no host filesystem, and no Docker socket** -- the isolation
boundary IronClaw [proves holds](https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape),
not just promises.

## Install

```sh
pip install llama-index-tools-ironclaw
```

## Usage

```python
from llama_index.core.agent.workflow import FunctionAgent
from llama_index.llms.openai import OpenAI
from llama_index.tools.ironclaw import IronClawToolSpec

tool_spec = IronClawToolSpec()  # points at the local IronClaw demo control-plane

agent = FunctionAgent(
    tools=tool_spec.to_tool_list(),
    llm=OpenAI(model="gpt-4o-mini"),
    system_prompt="Answer the user by running commands with the sandboxed_shell tool.",
)
```

Or call the tool directly:

```python
from llama_index.tools.ironclaw import IronClawToolSpec

tool_spec = IronClawToolSpec()
print(tool_spec.sandboxed_shell("id"))   # runs INSIDE the sandbox, not on your host
# [exit 0]
# uid=65532 gid=65532 groups=65532
```

`IronClawToolSpec` exposes a single tool, `sandboxed_shell(command: str)`, which
returns the command's combined stdout/stderr prefixed with its exit code. A
non-zero exit (for example, a blocked network call) is returned as data, never
raised -- a contained attack is observable output.

### Pointing at your control-plane

```python
IronClawToolSpec(
    addr="http://your-control-plane:8787",
    token="your-token",
    agent="your-agent-group",
)
```

By default it targets the offline demo control-plane (mock provider, no model
key, no channel tokens), so the whole thing runs **zero-credential**.

## Requirements

The tool `docker exec`s into a live IronClaw per-session sandbox, so it needs:

- A running IronClaw control-plane (the runnable, one-command demo is in
  [`examples/integrations/llamaindex`](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/llamaindex)).
- The Docker CLI on `PATH`.

The client itself is pure standard library; the only runtime dependency this
package adds is `llama-index-core`.

## How it works

A chat message to the demo agent makes the IronClaw router launch that
conversation's per-session sandbox as a sibling container (`ic-sbx-*`). The tool
then `docker exec`s into that container as its own non-root uid (65532) -- the
exact privilege a fully-jailbroken agent with arbitrary code execution would
have. This is the same boundary IronClaw's red-team-escape harness attacks:
`network=none`, no Docker socket, host filesystem not mounted, non-root,
read-only rootfs.

See the [IronClaw repo](https://github.com/IronSecCo/ironclaw) and the
[LlamaIndex integration example](https://github.com/IronSecCo/ironclaw/tree/main/examples/integrations/llamaindex)
for the full isolation model and a live PASS/FAIL containment demo.

## License

MIT
