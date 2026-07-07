import type { ICredentialType, INodeProperties } from 'n8n-workflow';

/**
 * Connection details for an IronClaw sandbox MCP server (`ironctl mcp serve`).
 *
 * The server binds loopback by default and needs no token there; a token is
 * required only when it is exposed on a routable address (IronClaw refuses to
 * bind a non-loopback address for `sandbox_exec` without one). The token, when
 * set, is sent as a bearer credential.
 */
export class IronClawSandboxApi implements ICredentialType {
	name = 'ironClawSandboxApi';

	displayName = 'IronClaw Sandbox API';

	documentationUrl = 'https://ironsec.co/docs/integrations/mcp-server';

	properties: INodeProperties[] = [
		{
			displayName: 'Endpoint',
			name: 'endpoint',
			type: 'string',
			default: 'http://127.0.0.1:9000',
			required: true,
			placeholder: 'http://127.0.0.1:9000',
			description:
				'Base URL of the IronClaw MCP server. Start one with `ironctl mcp serve --http :9000` (loopback by default).',
		},
		{
			displayName: 'Auth Token',
			name: 'token',
			type: 'string',
			typeOptions: { password: true },
			default: '',
			description:
				'Bearer token, required only when the server is bound to a non-loopback address. Leave empty for a local loopback server.',
		},
	];
}
