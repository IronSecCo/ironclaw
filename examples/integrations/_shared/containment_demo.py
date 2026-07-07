"""Shared containment demo for the framework integration examples.

Given a `invoke(command) -> str` callable -- the framework's tool, called
exactly as its agent would call it -- run a benign task plus a battery of
escape / exfiltration attempts and print a PASS/FAIL table. Exit non-zero if any
containment expectation is not met, so `run.sh` doubles as a CI-friendly check.

The point: the SAME agent tool that would be a host-shell foot-gun in a vanilla
setup is, when backed by an IronClaw sandbox, provably contained.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable


@dataclass
class Probe:
    title: str
    command: str
    # kind: "allow" -> should succeed; "contain" -> escape must be blocked.
    kind: str
    # contained(output) -> True if the observed output shows the attack held.
    contained: Callable[[str], bool]


def _has(out: str, needle: str) -> bool:
    return needle.lower() in out.lower()


# The "agent-generated" work. One benign command proves the tool actually runs
# code; the rest are the classic ways untrusted agent code tries to break out.
PROBES: list[Probe] = [
    Probe(
        title="benign task: run agent code",
        command="echo 'hello from inside the IronClaw sandbox'; id; pwd",
        kind="allow",
        contained=lambda out: _has(out, "hello from inside") and _has(out, "uid=65532"),
    ),
    Probe(
        title="network egress: only loopback exists",
        # Enumerate via /proc/net/dev, not /sys/class/net: gVisor (the runtime
        # this smoke requires) does not populate sysfs class dirs, but
        # /proc/net/dev is present on both runsc and runc and lists only `lo`
        # when network=none.
        command="tail -n +3 /proc/net/dev | cut -d: -f1 | tr -d ' ' | tr '\\n' ' '",
        kind="contain",
        contained=lambda out: out.split("[exit", 1)[-1].strip().endswith("lo")
        or out.strip().split("\n")[-1].strip() == "lo",
    ),
    Probe(
        title="network egress: DNS lookup of api.anthropic.com fails",
        command="getent hosts api.anthropic.com >/dev/null 2>&1 && echo RESOLVED || echo NO_EGRESS",
        kind="contain",
        contained=lambda out: _has(out, "no_egress") and not _has(out, "resolved"),
    ),
    Probe(
        title="host escape: Docker Engine socket is absent",
        command="[ -S /var/run/docker.sock ] || [ -S /run/docker.sock ] && echo PRESENT || echo ABSENT",
        kind="contain",
        contained=lambda out: _has(out, "absent") and not _has(out, "present"),
    ),
    Probe(
        title="host escape: host filesystem is not mounted",
        command="cat /etc/ironclaw-host-marker 2>/dev/null || ls /host 2>/dev/null || echo CONTAINED",
        kind="contain",
        contained=lambda out: _has(out, "contained"),
    ),
]


def run_containment_demo(invoke: Callable[[str], str], framework: str) -> int:
    """Drive `invoke` over every probe, print the table, return an exit code."""
    print(f"\n=== {framework} agent -> IronClaw sandbox: containment demo ===\n")
    print("Every command below is executed INSIDE a live per-session IronClaw")
    print("sandbox via the framework's own tool interface -- not on the host.\n")

    rows: list[tuple[str, str, str]] = []
    failed = False
    for p in PROBES:
        try:
            output = invoke(p.command)
        except Exception as exc:  # a tool that blew up is a failure to report
            output = f"(tool raised: {exc})"
        ok = p.contained(output)
        if p.kind == "allow":
            verdict = "PASS" if ok else "FAIL"
        else:  # contain
            verdict = "PASS" if ok else "FAIL"
        if verdict == "FAIL":
            failed = True
        rows.append((verdict, p.title, output.replace("\n", " ")[:70]))

    width = max(len(t) for _, t, _ in rows)
    for verdict, title, observed in rows:
        mark = "OK " if verdict == "PASS" else "XX "
        print(f"  [{mark}] {title.ljust(width)}  ->  {observed}")

    print()
    if failed:
        print("RESULT: FAIL -- a containment expectation did not hold.")
        return 1
    print("RESULT: PASS -- benign code ran; every escape attempt was contained.")
    print(f"\n{framework} agents get real code execution with none of the host risk.")
    return 0
