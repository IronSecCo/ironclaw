const { src, dest } = require('gulp');

/**
 * Copy node/credential SVG (and PNG) icons into dist/ next to their compiled
 * .node.js, mirroring the source tree. tsc emits only JS; n8n loads the icon
 * referenced by `icon: 'file:ironclaw.svg'` from the same dist folder.
 */
function buildIcons() {
	return src('nodes/**/*.{png,svg}').pipe(dest('dist/nodes'));
}

exports['build:icons'] = buildIcons;
exports.default = buildIcons;
