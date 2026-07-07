/**
 * Minimal client for IronClaw's `sandbox_exec` MCP tool over streamable HTTP.
 *
 * `ironctl mcp serve --http <addr>` exposes exactly one tool, `sandbox_exec`,
 * which runs a command inside an ephemeral, hardened IronClaw box (gVisor/runsc
 * by default; network=none, cap-drop ALL, non-root, read-only rootfs, seccomp)
 * and returns the command's stdout, stderr, exit code, and a containment summary.
 * The HTTP transport is a single stateless JSON-RPC POST -- no session handshake
 * is required for a `tools/call`, so this stays dependency-free (global `fetch`).
 *
 * This module is deliberately free of any n8n import so it can be driven directly
 * by the containment smoke (run.ts) as well as by the node's `execute()`.
 */

/** Parsed result of one `sandbox_exec` call. */
export interface SandboxExecResult {
	/** Command exit code inside the sandbox (0 = success). */
	exitCode: number;
	/** Standard output captured from the sandbox. */
	stdout: string;
	/** Standard error captured from the sandbox. */
	stderr: string;
	/** The controls IronClaw enforced (runtime, network, caps, user, ...). */
	containment: string;
	/** The image the box ran, as reported by the server. */
	image: string;
	/** The raw text block the tool returned (before parsing). */
	raw: string;
}

/** Raised when the MCP server is unreachable or returns a protocol-level error. */
export class SandboxError extends Error {
	constructor(message: string) {
		super(message);
		this.name = 'SandboxError';
	}
}

export interface SandboxExecParams {
	/** MCP server base URL, e.g. http://127.0.0.1:9000. */
	endpoint: string;
	/** Optional bearer token (required only for a non-loopback server). */
	token?: string;
	/** Command run inside the box via `sh -c`. */
	command: string;
	/** Optional container image override (server default when empty). */
	image?: string;
	/** Optional per-exec timeout in seconds. */
	timeoutSeconds?: number;
	/** Client-side HTTP timeout in milliseconds (default 120000). */
	httpTimeoutMs?: number;
}

/** The subset of `fetch` this client needs; lets the node inject n8n's helper. */
export type FetchLike = (
	url: string,
	init: {
		method: string;
		headers: Record<string, string>;
		body: string;
		signal?: AbortSignal;
	},
) => Promise<{ ok: boolean; status: number; text: () => Promise<string> }>;

interface JsonRpcResponse {
	result?: {
		content?: Array<{ type: string; text?: string }>;
		isError?: boolean;
	};
	error?: { code: number; message: string };
}

let callId = 0;

/**
 * Call `sandbox_exec` once and return the parsed result.
 *
 * A non-zero *command* exit is data, returned in `exitCode`/`stderr` -- not an
 * error. A `SandboxError` is thrown only when the sandbox itself cannot be
 * engaged (server unreachable, auth rejected, tool-level launch failure).
 */
export async function sandboxExec(
	params: SandboxExecParams,
	fetchImpl: FetchLike = fetch as unknown as FetchLike,
): Promise<SandboxExecResult> {
	const base = params.endpoint.replace(/\/+$/, '');
	const args: Record<string, unknown> = { command: params.command };
	if (params.image && params.image.trim() !== '') args.image = params.image.trim();
	if (params.timeoutSeconds && params.timeoutSeconds > 0) {
		args.timeout_seconds = params.timeoutSeconds;
	}

	const body = JSON.stringify({
		jsonrpc: '2.0',
		id: ++callId,
		method: 'tools/call',
		params: { name: 'sandbox_exec', arguments: args },
	});

	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		Accept: 'application/json, text/event-stream',
	};
	if (params.token && params.token.trim() !== '') {
		headers.Authorization = `Bearer ${params.token.trim()}`;
	}

	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), params.httpTimeoutMs ?? 120_000);
	let resp: Awaited<ReturnType<FetchLike>>;
	try {
		resp = await fetchImpl(base, {
			method: 'POST',
			headers,
			body,
			signal: controller.signal,
		});
	} catch (err) {
		throw new SandboxError(
			`could not reach ironctl mcp serve at ${base}: ${String(err)} ` +
				'(is `ironctl mcp serve --http` running and the endpoint correct?)',
		);
	} finally {
		clearTimeout(timer);
	}

	const text = await resp.text();
	if (!resp.ok) {
		throw new SandboxError(`MCP server at ${base} returned HTTP ${resp.status}: ${text.slice(0, 300)}`);
	}

	let parsed: JsonRpcResponse;
	try {
		parsed = JSON.parse(text) as JsonRpcResponse;
	} catch {
		throw new SandboxError(`MCP server returned a non-JSON response: ${text.slice(0, 300)}`);
	}
	if (parsed.error) {
		throw new SandboxError(`sandbox_exec JSON-RPC error ${parsed.error.code}: ${parsed.error.message}`);
	}
	const result = parsed.result;
	const raw = (result?.content ?? [])
		.filter((c) => c.type === 'text' && typeof c.text === 'string')
		.map((c) => c.text as string)
		.join('\n');
	// A tool-level launch failure (runsc missing, image pull denied) is surfaced
	// with isError; that means the sandbox never ran, so treat it as a hard error.
	if (result?.isError) {
		throw new SandboxError(`sandbox_exec could not launch a box: ${raw.slice(0, 400)}`);
	}
	return parseExecText(raw);
}

/**
 * Parse the `sandbox_exec` text block into fields. Shape (see formatExecResult in
 * cmd/ironctl/mcp_serve.go):
 *
 *     exit_code: 0
 *     containment: runtime=runsc (gVisor: ...), network=none, ...
 *     image: docker.io/library/alpine:3.20
 *     --- stdout ---
 *     <stdout>
 *     --- stderr ---
 *     <stderr>
 */
export function parseExecText(raw: string): SandboxExecResult {
	const exitMatch = raw.match(/^exit_code:\s*(-?\d+)/m);
	const containMatch = raw.match(/^containment:\s*(.*)$/m);
	const imageMatch = raw.match(/^image:\s*(.*)$/m);

	let stdout = '';
	let stderr = '';
	const stdoutIdx = raw.indexOf('--- stdout ---');
	const stderrIdx = raw.indexOf('--- stderr ---');
	if (stdoutIdx !== -1) {
		const end = stderrIdx !== -1 ? stderrIdx : raw.length;
		stdout = raw.slice(stdoutIdx + '--- stdout ---'.length, end).replace(/^\n/, '').replace(/\n$/, '');
	}
	if (stderrIdx !== -1) {
		stderr = raw.slice(stderrIdx + '--- stderr ---'.length).replace(/^\n/, '').replace(/\n$/, '');
	}

	return {
		exitCode: exitMatch ? parseInt(exitMatch[1], 10) : 0,
		stdout,
		stderr,
		containment: containMatch ? containMatch[1].trim() : '',
		image: imageMatch ? imageMatch[1].trim() : '',
		raw,
	};
}
