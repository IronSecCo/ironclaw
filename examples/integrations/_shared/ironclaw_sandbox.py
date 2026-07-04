"""IronClaw sandbox client for agent frameworks.

Run untrusted, agent-generated commands inside a live IronClaw per-session
sandbox instead of on the host. This is the piece a LangChain / CrewAI tool
wraps: the framework hands us a command string, we execute it inside the
isolated sandbox and hand back stdout + exit code.

Zero credentials. It talks to the offline demo control-plane (the same path as
docker-compose.demo.yml -- mock provider, no model key, no channel tokens).

Execution primitive. A chat message to the demo agent makes the router launch
that conversation's per-session sandbox as a sibling container (ic-sbx-*). We
then `docker exec` into that container as its own non-root uid (65532) -- the
exact privilege a fully-jailbroken agent with arbitrary code execution would
have. This is the same boundary IronClaw's red-team-escape harness proves holds:
network=none, no Docker socket, host filesystem not mounted, non-root, read-only
rootfs. See examples/red-team-escape/.

Pure standard library -- the only third-party dependency an integration adds is
the framework itself (langchain / crewai).
"""

from __future__ import annotations

import json
import subprocess
import time
import urllib.error
import urllib.request
from dataclasses import dataclass


@dataclass
class ExecResult:
    """Result of running one command inside the sandbox."""

    stdout: str
    exit_code: int

    def __str__(self) -> str:  # what a tool typically returns to the agent
        return f"[exit {self.exit_code}]\n{self.stdout}".rstrip()


class SandboxError(RuntimeError):
    """The sandbox could not be engaged or reached."""


class IronClawSandbox:
    """A handle to one live IronClaw per-session sandbox.

    Typical use::

        with IronClawSandbox() as sbx:
            print(sbx.run("id"))              # runs INSIDE the sandbox

    The control-plane lifecycle (build image, `docker compose up`) is owned by
    the example's run.sh; this client assumes the demo control-plane is already
    healthy and simply engages a session against it.
    """

    def __init__(
        self,
        addr: str = "http://127.0.0.1:8787",
        token: str = "ironclaw-demo",
        agent: str = "mock-agent",
        exec_uid: str = "65532:65532",
    ) -> None:
        self.addr = addr.rstrip("/")
        self.token = token
        self.agent = agent
        self.exec_uid = exec_uid
        self.container: str | None = None

    # -- lifecycle ----------------------------------------------------------

    def __enter__(self) -> "IronClawSandbox":
        self.engage()
        return self

    def __exit__(self, *_exc: object) -> None:
        # The per-session container is reaped by the control-plane when the demo
        # is torn down (run.sh owns `docker compose down`); nothing to close.
        return None

    def _http(self, method: str, path: str, body: dict | None = None) -> bytes:
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(f"{self.addr}{path}", data=data, method=method)
        req.add_header("Authorization", f"Bearer {self.token}")
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.read()
        except urllib.error.URLError as exc:  # connection refused, timeout, 5xx
            raise SandboxError(f"{method} {path} failed: {exc}") from exc

    def engage(self, timeout: int = 180) -> str:
        """Launch this session's sandbox and return its container name.

        Sends a chat to the demo agent (which makes the router spin up the
        per-session sandbox), then polls `docker ps` until the ic-sbx-*
        container is running. Idempotent-ish: re-engaging just finds the
        already-running container.
        """
        marker = f"integrations-sandbox engage {int(time.time())}"
        self._http(
            "POST",
            "/v1/ui/chat/send",
            {"agentGroupID": self.agent, "text": marker},
        )

        deadline = time.time() + timeout
        while time.time() < deadline:
            # Drain the reply so the agent loop keeps advancing, then look for
            # the running sandbox container for this session.
            try:
                self._http("GET", f"/v1/ui/chat/{self.agent}/messages")
            except SandboxError:
                pass
            name = self._find_container()
            if name:
                self.container = name
                return name
            time.sleep(1)
        raise SandboxError(
            f"no running sandbox container (ic-sbx-*) appeared within {timeout}s -- "
            "is the demo control-plane up? (docker compose -f docker-compose.demo.yml up)"
        )

    @staticmethod
    def _find_container() -> str | None:
        out = subprocess.run(
            [
                "docker", "ps",
                "--filter", "label=ironclaw.session",
                "--filter", "name=ic-sbx-",
                "--filter", "status=running",
                "--format", "{{.Names}}",
            ],
            capture_output=True,
            text=True,
        )
        names = [n for n in out.stdout.splitlines() if n.strip()]
        return names[0] if names else None

    # -- execution ----------------------------------------------------------

    def run(self, command: str, timeout: int = 30) -> ExecResult:
        """Execute a shell command INSIDE the sandbox and capture the result.

        Runs as the sandbox's own non-root uid, exactly what a jailbroken agent
        would have. Never raises on a non-zero command exit -- a contained
        attack is data, returned in ExecResult.exit_code / .stdout. Raises
        SandboxError only if the sandbox itself is unreachable.
        """
        if not self.container:
            self.engage()
        assert self.container is not None
        try:
            out = subprocess.run(
                [
                    "docker", "exec",
                    "-u", self.exec_uid,
                    self.container,
                    "sh", "-c", command,
                ],
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired:
            return ExecResult(stdout="(command timed out)", exit_code=124)
        except FileNotFoundError as exc:  # docker not installed
            raise SandboxError("docker CLI not found on PATH") from exc
        combined = (out.stdout + out.stderr).rstrip()
        return ExecResult(stdout=combined, exit_code=out.returncode)
