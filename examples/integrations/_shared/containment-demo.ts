/**
 * Shared containment demo for the JS/TS framework integration examples.
 *
 * Given an `invoke(command) => string` callable -- the framework's tool, called
 * exactly as its agent would call it -- run a benign task plus a battery of
 * escape / exfiltration attempts and print a PASS/FAIL table. Returns a non-zero
 * exit code if any containment expectation is not met, so `run.sh` doubles as a
 * CI-friendly check.
 *
 * The point: the SAME agent tool that would be a host-shell foot-gun in a vanilla
 * setup is, when backed by an IronClaw sandbox, provably contained. This is the
 * TS twin of _shared/containment_demo.py -- the probes are byte-for-byte the same
 * so both language ecosystems assert the identical boundary.
 */

export interface Probe {
  title: string;
  command: string;
  /** "allow" -> should succeed; "contain" -> escape must be blocked. */
  kind: "allow" | "contain";
  /** True if the observed output shows the attack held. */
  contained: (out: string) => boolean;
}

const has = (out: string, needle: string): boolean =>
  out.toLowerCase().includes(needle.toLowerCase());

/**
 * The "agent-generated" work. One benign command proves the tool actually runs
 * code; the rest are the classic ways untrusted agent code tries to break out.
 */
export const PROBES: Probe[] = [
  {
    title: "benign task: run agent code",
    command: "echo 'hello from inside the IronClaw sandbox'; id; pwd",
    kind: "allow",
    contained: (out) => has(out, "hello from inside") && has(out, "uid=65532"),
  },
  {
    title: "network egress: only loopback exists",
    // Enumerate via /proc/net/dev, not /sys/class/net: gVisor (the runtime this
    // smoke requires) does not populate sysfs class dirs, but /proc/net/dev is
    // present on both runsc and runc and lists only `lo` when network=none.
    command: "tail -n +3 /proc/net/dev | cut -d: -f1 | tr -d ' ' | tr '\\n' ' '",
    kind: "contain",
    contained: (out) => {
      const afterExit = out.split("[exit").pop()?.trim() ?? "";
      const lastLine = out.trim().split("\n").pop()?.trim() ?? "";
      return afterExit.endsWith("lo") || lastLine === "lo";
    },
  },
  {
    title: "network egress: DNS lookup of api.anthropic.com fails",
    command:
      "getent hosts api.anthropic.com >/dev/null 2>&1 && echo RESOLVED || echo NO_EGRESS",
    kind: "contain",
    contained: (out) => has(out, "no_egress") && !has(out, "resolved"),
  },
  {
    title: "host escape: Docker Engine socket is absent",
    command:
      "[ -S /var/run/docker.sock ] || [ -S /run/docker.sock ] && echo PRESENT || echo ABSENT",
    kind: "contain",
    contained: (out) => has(out, "absent") && !has(out, "present"),
  },
  {
    title: "host escape: host filesystem is not mounted",
    command:
      "cat /etc/ironclaw-host-marker 2>/dev/null || ls /host 2>/dev/null || echo CONTAINED",
    kind: "contain",
    contained: (out) => has(out, "contained"),
  },
];

/** Drive `invoke` over every probe, print the table, return an exit code. */
export async function runContainmentDemo(
  invoke: (command: string) => string | Promise<string>,
  framework: string,
): Promise<number> {
  console.log(`\n=== ${framework} agent -> IronClaw sandbox: containment demo ===\n`);
  console.log("Every command below is executed INSIDE a live per-session IronClaw");
  console.log("sandbox via the framework's own tool interface -- not on the host.\n");

  const rows: Array<{ verdict: "PASS" | "FAIL"; title: string; observed: string }> = [];
  let failed = false;
  for (const p of PROBES) {
    let output: string;
    try {
      output = await invoke(p.command);
    } catch (exc) {
      output = `(tool raised: ${String(exc)})`; // a tool that blew up is a failure
    }
    const verdict = p.contained(output) ? "PASS" : "FAIL";
    if (verdict === "FAIL") failed = true;
    rows.push({
      verdict,
      title: p.title,
      observed: output.replace(/\n/g, " ").slice(0, 70),
    });
  }

  const width = Math.max(...rows.map((r) => r.title.length));
  for (const { verdict, title, observed } of rows) {
    const mark = verdict === "PASS" ? "OK " : "XX ";
    console.log(`  [${mark}] ${title.padEnd(width)}  ->  ${observed}`);
  }

  console.log();
  if (failed) {
    console.log("RESULT: FAIL -- a containment expectation did not hold.");
    return 1;
  }
  console.log("RESULT: PASS -- benign code ran; every escape attempt was contained.");
  console.log(`\n${framework} agents get real code execution with none of the host risk.`);
  return 0;
}
