/**
 * IronClaw sandbox client for JS/TS agent frameworks.
 *
 * Run untrusted, agent-generated commands inside a live IronClaw per-session
 * sandbox instead of on the host. This is the piece a Vercel AI SDK / LangChain.js
 * tool wraps: the framework hands us a command string, we execute it inside the
 * isolated sandbox and hand back stdout + exit code.
 *
 * Zero credentials. It talks to the offline demo control-plane (the same path as
 * docker-compose.demo.yml -- mock provider, no model key, no channel tokens).
 *
 * Execution primitive. A chat message to the demo agent makes the router launch
 * that conversation's per-session sandbox as a sibling container (ic-sbx-*). We
 * then `docker exec` into that container as its own non-root uid (65532) -- the
 * exact privilege a fully-jailbroken agent with arbitrary code execution would
 * have. This is the same boundary IronClaw's red-team-escape harness proves
 * holds: network=none, no Docker socket, host filesystem not mounted, non-root,
 * read-only rootfs. See examples/red-team-escape/.
 *
 * Pure Node standard library (global fetch + child_process) -- the only
 * third-party dependency an integration adds is the framework itself (ai /
 * @langchain/core). This is the TS twin of _shared/ironclaw_sandbox.py.
 */

import { spawnSync } from "node:child_process";

/** Result of running one command inside the sandbox. */
export class ExecResult {
  constructor(
    readonly stdout: string,
    readonly exitCode: number,
  ) {}

  /** What a tool typically returns to the agent. */
  toString(): string {
    return `[exit ${this.exitCode}]\n${this.stdout}`.replace(/\s+$/, "");
  }
}

/** The sandbox could not be engaged or reached. */
export class SandboxError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "SandboxError";
  }
}

export interface SandboxOptions {
  /** Control-plane base URL. Default http://127.0.0.1:8787. */
  addr?: string;
  /** API bearer token. Default "ironclaw-demo". */
  token?: string;
  /** Agent group id to chat with. Default "mock-agent". */
  agent?: string;
  /** uid:gid to docker-exec as. Default "65532:65532" (non-root). */
  execUid?: string;
}

const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

/**
 * A handle to one live IronClaw per-session sandbox.
 *
 * Typical use:
 *
 *     const sandbox = await new IronClawSandbox().engage();
 *     console.log((await sandbox.run("id")).toString()); // runs INSIDE the sandbox
 *
 * The control-plane lifecycle (build image, `docker compose up`) is owned by the
 * example's run.sh; this client assumes the demo control-plane is already healthy
 * and simply engages a session against it.
 */
export class IronClawSandbox {
  readonly addr: string;
  readonly token: string;
  readonly agent: string;
  readonly execUid: string;
  container: string | null = null;

  constructor(opts: SandboxOptions = {}) {
    this.addr = (opts.addr ?? "http://127.0.0.1:8787").replace(/\/+$/, "");
    this.token = opts.token ?? "ironclaw-demo";
    this.agent = opts.agent ?? "mock-agent";
    this.execUid = opts.execUid ?? "65532:65532";
  }

  private async http(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<string> {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.token}`,
    };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    let resp: Response;
    try {
      resp = await fetch(`${this.addr}${path}`, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: AbortSignal.timeout(15_000),
      });
    } catch (err) {
      // connection refused, timeout, DNS
      throw new SandboxError(`${method} ${path} failed: ${String(err)}`, {
        cause: err,
      });
    }
    if (!resp.ok) {
      throw new SandboxError(`${method} ${path} failed: HTTP ${resp.status}`);
    }
    return resp.text();
  }

  /**
   * Launch this session's sandbox and return `this` once its container is up.
   *
   * Sends a chat to the demo agent (which makes the router spin up the
   * per-session sandbox), then polls `docker ps` until the ic-sbx-* container is
   * running. Idempotent-ish: re-engaging just finds the already-running
   * container.
   */
  async engage(timeoutSec = 180): Promise<this> {
    // Vary the marker per call without Date.now (portable to sandboxed runners).
    const marker = `integrations-sandbox engage ${process.pid}-${engageCounter++}`;
    await this.http("POST", "/v1/ui/chat/send", {
      agentGroupID: this.agent,
      text: marker,
    });

    const deadline = Date.now() + timeoutSec * 1000;
    while (Date.now() < deadline) {
      // Drain the reply so the agent loop keeps advancing, then look for the
      // running sandbox container for this session.
      try {
        await this.http("GET", `/v1/ui/chat/${this.agent}/messages`);
      } catch {
        /* transient; keep polling */
      }
      const name = findContainer();
      if (name) {
        this.container = name;
        return this;
      }
      await sleep(1000);
    }
    throw new SandboxError(
      `no running sandbox container (ic-sbx-*) appeared within ${timeoutSec}s -- ` +
        "is the demo control-plane up? (docker compose -f docker-compose.demo.yml up)",
    );
  }

  /**
   * Execute a shell command INSIDE the sandbox and capture the result.
   *
   * Runs as the sandbox's own non-root uid, exactly what a jailbroken agent
   * would have. Never rejects on a non-zero command exit -- a contained attack
   * is data, returned in ExecResult.exitCode / .stdout. Throws SandboxError only
   * if the sandbox itself is unreachable.
   */
  async run(command: string, timeoutSec = 30): Promise<ExecResult> {
    if (!this.container) await this.engage();
    const out = spawnSync(
      "docker",
      ["exec", "-u", this.execUid, this.container as string, "sh", "-c", command],
      { encoding: "utf8", timeout: timeoutSec * 1000 },
    );
    if (out.error) {
      const err = out.error as NodeJS.ErrnoException;
      if (err.code === "ETIMEDOUT") {
        return new ExecResult("(command timed out)", 124);
      }
      if (err.code === "ENOENT") {
        throw new SandboxError("docker CLI not found on PATH", { cause: err });
      }
      throw new SandboxError(`docker exec failed: ${err.message}`, { cause: err });
    }
    const combined = `${out.stdout ?? ""}${out.stderr ?? ""}`.replace(/\s+$/, "");
    return new ExecResult(combined, out.status ?? 0);
  }
}

let engageCounter = 0;

/** Find the running per-session sandbox container, if any. */
function findContainer(): string | null {
  const out = spawnSync(
    "docker",
    [
      "ps",
      "--filter",
      "label=ironclaw.session",
      "--filter",
      "name=ic-sbx-",
      "--filter",
      "status=running",
      "--format",
      "{{.Names}}",
    ],
    { encoding: "utf8" },
  );
  if (out.error || out.status !== 0) return null;
  const names = (out.stdout ?? "")
    .split("\n")
    .map((n) => n.trim())
    .filter(Boolean);
  return names.length ? names[0] : null;
}
