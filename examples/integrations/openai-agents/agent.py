#!/usr/bin/env python3
"""OpenAI Agents SDK agent whose code-execution tool runs inside an IronClaw sandbox.

The value: the OpenAI Agents SDK is great at *planning* tool calls, but its tools run
in your process, with your key in memory and open egress. Here the agent's one tool —
``sandbox_bash`` — executes inside a real, sealed IronClaw per-session sandbox instead:
no network card, no host filesystem, no Docker socket. Same agent you designed; a
perimeter it never had.

Two ways to run, picked automatically:

* Zero-credential (default). No ``openai-agents`` install and no API key needed: we
  drive the exact same tool function through a short scripted transcript (a "mock LLM")
  so you can watch the sealed loop — a benign command that works, then an escape that
  is blocked — with nothing but Python + Docker.
* Real SDK. If ``agents`` is importable and ``OPENAI_API_KEY`` is set, the real
  ``Runner`` plans the tool calls and the model decides when to reach for the sandbox.

Either way the tool code below is genuine OpenAI Agents SDK usage.
"""

from __future__ import annotations

import os
import sys

# Make the shared shim importable whether run from its dir or via run.sh (PYTHONPATH).
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from ironclaw_sandbox import (  # noqa: E402
    BOLD, CYAN, DIM, GREEN, RESET, YELLOW,
    SandboxSession, print_containment,
)

# One live sandbox backs every tool call in this run.
_SESSION: SandboxSession | None = None


def _session() -> SandboxSession:
    global _SESSION
    if _SESSION is None:
        print(f"{DIM}==> engaging a real IronClaw sandbox (per-session container)...{RESET}")
        _SESSION = SandboxSession.engage()
        print(f"{DIM}    sandbox up: {_SESSION.container}{RESET}")
    return _SESSION


# --- the tool: genuine OpenAI Agents SDK, backed by the sandbox --------------
# `agents` is the OpenAI Agents SDK package (pip install openai-agents). When it is not
# installed we fall back to a no-op decorator so the SAME function is callable directly
# by the zero-credential scripted runner below.
try:
    from agents import function_tool  # type: ignore
    _HAVE_SDK = True
except ImportError:  # zero-credential path: identity decorator, same function body
    _HAVE_SDK = False

    def function_tool(fn):  # type: ignore
        return fn


def _sandbox_bash_impl(command: str) -> str:
    """Run a command inside the sealed sandbox. The raw callable used by both paths."""
    rc, out = _session().exec(command)
    return f"(exit {rc})\n{out}" if out else f"(exit {rc}, no output)"


@function_tool
def sandbox_bash(command: str) -> str:
    """Run a shell command inside the sealed IronClaw sandbox and return its output.

    Use this for ANY code or shell execution. It runs in a sandbox with no network and
    no host access, so it is safe even for untrusted or agent-generated commands.
    """
    # When openai-agents is installed, function_tool wraps this into a FunctionTool that
    # is NOT directly callable, so the scripted path calls _sandbox_bash_impl instead.
    return _sandbox_bash_impl(command)


def _speak_agent(text: str) -> None:
    print(f"{BOLD}{CYAN}agent>{RESET} {text}")


def _speak_tool(command: str, result: str) -> None:
    print(f"{DIM}  -> sandbox_bash({command!r}){RESET}")
    for line in result.splitlines():
        print(f"{DIM}     {line}{RESET}")


# --- zero-credential scripted runner (mock LLM) -----------------------------
def run_scripted() -> int:
    """Drive the sandbox-backed tool through a fixed transcript. No key, no SDK, no network."""
    print(f"\n{BOLD}OpenAI Agents SDK -> IronClaw sandbox{RESET} "
          f"{DIM}(zero-credential scripted transcript){RESET}\n")

    # 1) A benign task the agent completes by executing in the box.
    _speak_agent("I'll gather the box's identity by running a shell command in the sandbox.")
    result = _sandbox_bash_impl("uname -a; id; echo sealed-workspace: $(pwd)")
    _speak_tool("uname -a; id; echo sealed-workspace: $(pwd)", result)
    _speak_agent("Done - that ran inside the sealed sandbox, not on your host.")

    # 2) The agent (or a prompt injection) turns malicious. Show the box hold.
    print(f"\n{YELLOW}Now a prompt-injected instruction makes the agent try to break out.{RESET}")
    probes = _session().containment_report()
    ok = print_containment(probes)

    _session().close()
    print()
    if ok:
        print(f"{GREEN}{BOLD}PASS{RESET} - the agent's tool ran real code in a sandbox that "
              f"a jailbroken agent could not escape.")
        return 0
    print("FAIL - an escape was not contained (see above).")
    return 1


# --- real OpenAI Agents SDK runner ------------------------------------------
def run_sdk() -> int:
    """Let the real OpenAI Agents SDK Runner plan tool calls against the sandbox."""
    from agents import Agent, Runner  # type: ignore

    agent = Agent(
        name="Sandboxed Operator",
        instructions=(
            "You are a careful operator. Use the sandbox_bash tool for ALL shell/code "
            "execution - never assume you can run commands any other way. First report "
            "the box's identity (uname, id, working dir). Then, to demonstrate isolation, "
            "attempt to resolve api.anthropic.com and read /host/etc/shadow, and report "
            "plainly that both are blocked by the sandbox."
        ),
        tools=[sandbox_bash],
        model=os.environ.get("OPENAI_MODEL", "gpt-4o-mini"),
    )
    result = Runner.run_sync(
        agent, "Show me this environment and prove it is isolated.")
    print(f"\n{BOLD}{CYAN}agent final>{RESET} {result.final_output}")

    # Always print the deterministic containment table too - proof, not just prose.
    probes = _session().containment_report()
    ok = print_containment(probes)
    _session().close()
    return 0 if ok else 1


def main() -> int:
    use_sdk = _HAVE_SDK and os.environ.get("OPENAI_API_KEY")
    if use_sdk:
        print(f"{DIM}==> OpenAI Agents SDK detected + OPENAI_API_KEY set: real runner.{RESET}")
        return run_sdk()
    if not _HAVE_SDK:
        print(f"{DIM}==> openai-agents not installed: zero-credential scripted transcript.{RESET}")
        print(f"{DIM}    (pip install openai-agents and set OPENAI_API_KEY for the real runner.){RESET}")
    else:
        print(f"{DIM}==> OPENAI_API_KEY unset: zero-credential scripted transcript.{RESET}")
    return run_scripted()


if __name__ == "__main__":
    raise SystemExit(main())
