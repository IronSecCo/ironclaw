/**
 * Containment smoke for the n8n IronClaw Sandbox node.
 *
 * Drives the node's real execution path -- the `sandbox_exec` MCP client in
 * nodes/IronClawSandbox/McpClient.ts, exactly what IronClawSandbox.node.ts calls
 * at runtime -- against a live `ironctl mcp serve` endpoint. It runs one benign
 * command plus the shared battery of escape/exfiltration attempts and prints a
 * PASS/FAIL containment table, exiting non-zero if any expectation is not met.
 * run.sh stands up the MCP server; this asserts the boundary holds.
 *
 * The probes are the byte-identical shared set every IronClaw framework
 * integration asserts (_shared/containment-demo.ts), so the n8n node proves the
 * same isolation boundary as the Vercel AI SDK / LangChain.js / Python tools.
 */

import { runContainmentDemo } from '../_shared/containment-demo';
import { sandboxExec } from './nodes/IronClawSandbox/McpClient';

async function main(): Promise<number> {
	const endpoint = process.env.IRONCLAW_MCP_ADDR ?? 'http://127.0.0.1:9111';
	const token = process.env.IRONCLAW_MCP_AUTH_TOKEN ?? '';

	console.log(`==> driving the n8n IronClaw Sandbox node against ${endpoint}`);
	console.log('    (the same McpClient.sandbox_exec call the node makes at runtime)');

	// Invoke exactly as the node does, then normalize to the shared ExecResult
	// string shape ("[exit N]\n<combined output>") the probes assert against.
	const invoke = async (command: string): Promise<string> => {
		const r = await sandboxExec({ endpoint, token, command, timeoutSeconds: 30 });
		const combined = r.stderr ? `${r.stdout}\n${r.stderr}` : r.stdout;
		return `[exit ${r.exitCode}]\n${combined}`.replace(/\s+$/, '');
	};

	return runContainmentDemo(invoke, 'n8n');
}

main().then(
	(code) => process.exit(code),
	(err) => {
		console.error(err);
		process.exit(1);
	},
);
