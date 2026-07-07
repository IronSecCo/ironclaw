import type {
	IExecuteFunctions,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
} from 'n8n-workflow';
import { NodeOperationError } from 'n8n-workflow';

import { sandboxExec, SandboxError, type FetchLike } from './McpClient';

/**
 * IronClaw Sandbox node.
 *
 * Runs a shell command or code snippet inside an ephemeral, hardened IronClaw
 * sandbox box (via the `sandbox_exec` MCP tool) instead of on the n8n host. Use
 * it whenever a workflow must execute untrusted or model-generated code: prompt
 * output, a webhook payload, an LLM tool call. The box has no network, drops all
 * capabilities, runs non-root under a read-only rootfs, and is torn down after
 * the command returns.
 */
export class IronClawSandbox implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'IronClaw Sandbox',
		name: 'ironClawSandbox',
		icon: 'file:ironclaw.svg',
		group: ['transform'],
		version: 1,
		subtitle: '={{$parameter["language"]}}',
		description: 'Run untrusted code inside an isolated IronClaw sandbox',
		defaults: {
			name: 'IronClaw Sandbox',
		},
		inputs: ['main'],
		outputs: ['main'],
		credentials: [
			{
				name: 'ironClawSandboxApi',
				required: true,
			},
		],
		properties: [
			{
				displayName: 'Language',
				name: 'language',
				type: 'options',
				options: [
					{ name: 'Shell', value: 'shell' },
					{ name: 'Python', value: 'python' },
					{ name: 'Node.js', value: 'node' },
				],
				default: 'shell',
				description: 'How to interpret the code. Shell runs it directly; the others wrap it with an interpreter.',
			},
			{
				displayName: 'Code',
				name: 'code',
				type: 'string',
				typeOptions: {
					rows: 6,
				},
				default: '',
				required: true,
				placeholder: 'echo \'hello from inside the IronClaw sandbox\'; whoami',
				description: 'The command or code to run inside the sandbox',
			},
			{
				displayName: 'Image',
				name: 'image',
				type: 'string',
				default: '',
				placeholder: 'python:3.12-slim',
				description:
					'Container image override. Leave empty for the server default (alpine). Use e.g. python:3.12-slim for Python or node:20-slim for Node.js code.',
			},
			{
				displayName: 'Timeout (Seconds)',
				name: 'timeoutSeconds',
				type: 'number',
				typeOptions: {
					minValue: 1,
					maxValue: 600,
				},
				default: 30,
				description: 'Per-execution timeout that bounds a runaway command',
			},
			{
				displayName: 'Continue When Command Exits Non-Zero',
				name: 'continueOnNonZeroExit',
				type: 'boolean',
				default: true,
				description:
					'Whether a non-zero exit code from the sandboxed command is passed through as data (on) rather than raised as a node error (off). Sandbox-engagement failures always error.',
			},
		],
	};

	async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
		const items = this.getInputData();
		const returnData: INodeExecutionData[] = [];

		const credentials = await this.getCredentials('ironClawSandboxApi');
		const endpoint = (credentials.endpoint as string) ?? '';
		const token = (credentials.token as string) ?? '';

		// Route the HTTP call through n8n's own request helper so proxy settings,
		// TLS config, and request logging apply -- adapted to the tiny FetchLike
		// shape McpClient expects.
		const fetchImpl: FetchLike = async (url, init) => {
			const responseText = (await this.helpers.httpRequest({
				method: init.method as 'POST',
				url,
				headers: init.headers,
				body: init.body,
				json: false,
				returnFullResponse: false,
			})) as string;
			return {
				ok: true,
				status: 200,
				text: async () => responseText,
			};
		};

		for (let i = 0; i < items.length; i++) {
			const language = this.getNodeParameter('language', i) as string;
			const code = this.getNodeParameter('code', i) as string;
			const image = this.getNodeParameter('image', i, '') as string;
			const timeoutSeconds = this.getNodeParameter('timeoutSeconds', i, 30) as number;
			const continueOnNonZeroExit = this.getNodeParameter('continueOnNonZeroExit', i, true) as boolean;

			const command = wrapCode(language, code);

			try {
				const result = await sandboxExec(
					{ endpoint, token, command, image, timeoutSeconds },
					fetchImpl,
				);

				if (!continueOnNonZeroExit && result.exitCode !== 0) {
					throw new NodeOperationError(
						this.getNode(),
						`Sandboxed command exited with code ${result.exitCode}`,
						{ itemIndex: i, description: result.stderr || result.stdout },
					);
				}

				returnData.push({
					json: {
						exitCode: result.exitCode,
						stdout: result.stdout,
						stderr: result.stderr,
						containment: result.containment,
						image: result.image,
						language,
					},
					pairedItem: { item: i },
				});
			} catch (error) {
				if (error instanceof SandboxError) {
					if (this.continueOnFail()) {
						returnData.push({ json: { error: error.message }, pairedItem: { item: i } });
						continue;
					}
					throw new NodeOperationError(this.getNode(), error.message, { itemIndex: i });
				}
				throw error;
			}
		}

		return [returnData];
	}
}

/**
 * Wrap a code snippet for the requested interpreter. Shell runs verbatim; Python
 * and Node.js are piped to their interpreter over stdin so the caller never has
 * to hand-write the `python3 -c` / `node -e` boilerplate (and quoting is safe).
 */
function wrapCode(language: string, code: string): string {
	switch (language) {
		case 'python':
			return `python3 - <<'IRONCLAW_EOF'\n${code}\nIRONCLAW_EOF`;
		case 'node':
			return `node - <<'IRONCLAW_EOF'\n${code}\nIRONCLAW_EOF`;
		default:
			return code;
	}
}
