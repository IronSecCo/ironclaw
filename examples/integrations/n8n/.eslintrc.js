/**
 * ESLint config for the n8n community node, using the official
 * eslint-plugin-n8n-nodes-base ruleset. `npm run lint` checks the node,
 * credential, and package.json against n8n's community-node conventions.
 */
module.exports = {
	root: true,
	parser: '@typescript-eslint/parser',
	parserOptions: {
		sourceType: 'module',
		extraFileExtensions: ['.json'],
	},
	ignorePatterns: ['dist/**', 'node_modules/**', 'run.ts', 'gulpfile.js', '.eslintrc.js'],
	overrides: [
		{
			files: ['package.json'],
			plugins: ['eslint-plugin-n8n-nodes-base'],
			extends: ['plugin:n8n-nodes-base/community'],
			rules: {
				'n8n-nodes-base/community-package-json-name-still-default': 'off',
			},
		},
		{
			files: ['credentials/**/*.ts'],
			plugins: ['eslint-plugin-n8n-nodes-base'],
			extends: ['plugin:n8n-nodes-base/credentials'],
			rules: {
				// documentationUrl is a full HTTPS URL (satisfies -not-http-url), which
				// is the shape n8n renders as an external "Docs" link. The -miscased
				// rule wants a bare camelCase doc slug instead; the two are mutually
				// exclusive, so we keep the real URL and silence the slug rule.
				'n8n-nodes-base/cred-class-field-documentation-url-miscased': 'off',
			},
		},
		{
			files: ['nodes/**/*.ts'],
			plugins: ['eslint-plugin-n8n-nodes-base'],
			extends: ['plugin:n8n-nodes-base/nodes'],
		},
	],
};
