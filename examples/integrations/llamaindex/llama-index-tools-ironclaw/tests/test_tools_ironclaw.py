from unittest.mock import Mock

from llama_index.core.tools.tool_spec.base import BaseToolSpec

from llama_index.tools.ironclaw import IronClawToolSpec
from llama_index.tools.ironclaw._sandbox import ExecResult, IronClawSandbox


def test_class():
    names_of_base_classes = [b.__name__ for b in IronClawToolSpec.__mro__]
    assert BaseToolSpec.__name__ in names_of_base_classes


def test_spec_functions():
    assert IronClawToolSpec.spec_functions == ["sandboxed_shell"]


def test_default_sandbox_is_demo_control_plane():
    spec = IronClawToolSpec()
    assert isinstance(spec.sandbox, IronClawSandbox)
    assert spec.sandbox.addr == "http://127.0.0.1:8787"
    assert spec.sandbox.token == "ironclaw-demo"
    assert spec.sandbox.agent == "mock-agent"


def test_init_passes_params_to_sandbox():
    spec = IronClawToolSpec(
        addr="http://cp.example:9999/",
        token="tok",
        agent="my-agent",
    )
    assert spec.sandbox.addr == "http://cp.example:9999"  # trailing slash stripped
    assert spec.sandbox.token == "tok"
    assert spec.sandbox.agent == "my-agent"


def test_injected_sandbox_is_reused():
    injected = IronClawSandbox(addr="http://x:1")
    spec = IronClawToolSpec(sandbox=injected)
    assert spec.sandbox is injected


def test_sandboxed_shell_delegates_to_sandbox_run():
    fake = Mock(spec=IronClawSandbox)
    fake.run.return_value = ExecResult(stdout="hello from inside", exit_code=0)
    spec = IronClawToolSpec(sandbox=fake)

    result = spec.sandboxed_shell("echo hello")

    fake.run.assert_called_once_with("echo hello")
    assert result == "[exit 0]\nhello from inside"


def test_sandboxed_shell_returns_nonzero_exit_as_data():
    fake = Mock(spec=IronClawSandbox)
    fake.run.return_value = ExecResult(stdout="curl: could not resolve host", exit_code=6)
    spec = IronClawToolSpec(sandbox=fake)

    result = spec.sandboxed_shell("curl https://api.anthropic.com")

    assert result == "[exit 6]\ncurl: could not resolve host"


def test_to_tool_list_exposes_sandboxed_shell():
    spec = IronClawToolSpec(sandbox=Mock(spec=IronClawSandbox))
    tools = spec.to_tool_list()
    names = [t.metadata.name for t in tools]
    assert names == ["sandboxed_shell"]
