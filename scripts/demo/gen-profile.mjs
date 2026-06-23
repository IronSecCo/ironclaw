#!/usr/bin/env node
// gen-profile.mjs — emit an iTerm2 .itermcolors plist in the IronClaw steel-blue
// brand palette, for svg-term-cli (--profile). The 16 ANSI slots plus fg/bg/cursor
// govern the rendered colors; the cast's 256-color indices (67/110/153) are already
// steel-blue in the standard xterm palette and render brand-aligned regardless.
//
// Usage: node scripts/demo/gen-profile.mjs > scripts/demo/ironclaw-steel.itermcolors

const hex = (h) => {
  const n = parseInt(h.replace('#', ''), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => v / 255);
};

// brand palette (from docs/assets/logo.svg + social-preview.svg)
const COLORS = {
  Background: '#0b1124',   // deep navy
  Foreground: '#eaf2ff',   // near-white blue-tint
  Bold: '#ffffff',
  Cursor: '#63a0ff',
  CursorText: '#0b1124',
  SelectionText: '#eaf2ff',
  Selection: '#1d2c57',
  'Ansi 0': '#1d2c57',  'Ansi 8': '#3a4a7a',   // black / bright black
  'Ansi 1': '#ff8087',  'Ansi 9': '#ff9aa0',   // red
  'Ansi 2': '#5ad6a0',  'Ansi 10': '#7ce8bb',  // green (success / prompt)
  'Ansi 3': '#e3b341',  'Ansi 11': '#f0c460',  // yellow
  'Ansi 4': '#3b82f6',  'Ansi 12': '#63a0ff',  // blue
  'Ansi 5': '#b9a0ff',  'Ansi 13': '#cdbcff',  // magenta
  'Ansi 6': '#63a0ff',  'Ansi 14': '#93b6ff',  // cyan
  'Ansi 7': '#b9d4ff',  'Ansi 15': '#ffffff',  // white
};

const entry = (name, h) => {
  const [r, g, b] = hex(h);
  return `	<key>${name} Color</key>
	<dict>
		<key>Color Space</key>
		<string>sRGB</string>
		<key>Red Component</key>
		<real>${r.toFixed(6)}</real>
		<key>Green Component</key>
		<real>${g.toFixed(6)}</real>
		<key>Blue Component</key>
		<real>${b.toFixed(6)}</real>
		<key>Alpha Component</key>
		<real>1</real>
	</dict>`;
};

const body = Object.entries(COLORS).map(([k, v]) => entry(k, v)).join('\n');
process.stdout.write(
  `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
${body}
</dict>
</plist>
`);
