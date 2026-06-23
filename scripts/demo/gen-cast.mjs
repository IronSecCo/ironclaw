#!/usr/bin/env node
// gen-cast.mjs — deterministically build the IronClaw hero demo asciinema v2 cast.
//
// The cast is a faithful re-enactment of the validated zero-credential chat demo
// (docs/quickstart.md, shipped in IRO-27): one command brings up the offline
// `mock-agent` control-plane, a chat engages a real per-session sandbox container,
// and the reply flows back through the encrypted queue. The outputs below were
// captured verbatim from a live `docker-compose.demo.yml` run.
//
// Deterministic on purpose (no Date.now / random) so the committed cast + SVG stay
// reproducible. Usage:  node scripts/demo/gen-cast.mjs > docs/assets/demo.cast
//
// Honesty: the demo relaxes the sandbox to runc (shared kernel) on a default bridge
// network, so we do NOT claim network=none for the demo itself. The hardened
// production posture (gVisor + network=none) is named as the upgrade path.

const COLS = 92;
const ROWS = 26;

// ── ANSI helpers (real ESC bytes; xterm.js / svg-term renders these) ──────────
const E = '\x1b[';
const R = E + '0m';
const c = {
  prompt: (s) => E + '92m' + s + R,        // bright green  $
  ok: (s) => E + '92m' + s + R,            // success green
  addr: (s) => E + '38;5;110m' + s + R,    // steel-blue link / id
  reply: (s) => E + '38;5;153m' + s + R,   // light-blue agent reply
  dim: (s) => E + '38;5;67m' + s + R,      // muted steel-blue comment / annotation
  bold: (s) => E + '1m' + s + R,
};

const events = [];
let t = 0; // seconds, monotonic

const CHAR = 0.028;   // per typed character
const ENTER = 0.35;   // pause after Enter, before output
const LINE = 0.06;    // between streamed output lines
const BEAT = 0.7;     // narrative pause between blocks

const emit = (s) => events.push([Number(t.toFixed(3)), 'o', s]);
const wait = (s) => { t += s; };

// Type a command after a green prompt, char by char, then newline.
function type(cmd) {
  emit(c.prompt('$') + ' ');
  for (const ch of cmd) { wait(CHAR); emit(ch); }
  wait(ENTER);
  emit('\r\n');
}

// Continuation line of a wrapped command (indent, no prompt), typed char by char.
function cont(rest) {
  emit('       ');
  for (const ch of rest) { wait(CHAR); emit(ch); }
  wait(ENTER);
  emit('\r\n');
}

// A dim "# comment" narration line, revealed at once.
function comment(text) {
  emit(c.dim('# ' + text) + '\r\n');
  wait(0.25);
}

// Stream pre-colored output lines, one per LINE tick.
function out(lines) {
  for (const ln of lines) { wait(LINE); emit(ln + '\r\n'); }
}

// ── header ────────────────────────────────────────────────────────────────
const header = {
  version: 2,
  width: COLS,
  height: ROWS,
  timestamp: 1782182997, // 2026-06-23T02:49:57Z (capture time; fixed for determinism)
  idle_time_limit: 1.2,
  title: 'IronClaw — zero-credential chat demo',
  env: { SHELL: '/bin/zsh', TERM: 'xterm-256color' },
  // IronClaw steel-blue terminal theme (travels with the cast; honored by svg-term
  // and asciinema players). Palette = ANSI 0..15 from the brand palette.
  theme: {
    fg: '#eaf2ff',
    bg: '#0b1124',
    palette: [
      '#1d2c57', '#ff8087', '#5ad6a0', '#e3b341', // black red green yellow
      '#3b82f6', '#b9a0ff', '#63a0ff', '#b9d4ff', // blue magenta cyan white
      '#3a4a7a', '#ff9aa0', '#7ce8bb', '#f0c460', // bright black red green yellow
      '#63a0ff', '#cdbcff', '#93b6ff', '#ffffff', // bright blue magenta cyan white
    ].join(':'),
  },
};

// ── scene ──────────────────────────────────────────────────────────────────
wait(0.5);
comment('IronClaw zero-credential demo — no API key, no gVisor required.');
wait(0.2);

type('docker compose -f docker-compose.demo.yml up -d');
out([
  ' ' + c.ok('✔') + ' Container ironclaw-demo-controlplane-1  ' + c.ok('Started'),
  c.addr('http://127.0.0.1:8787') + '  ' + c.dim('·  offline mock-agent seeded, zero credentials'),
]);
wait(BEAT);

type('curl -sf localhost:8787/healthz');
out(['{"status":"' + c.ok('ok') + '"}']);
wait(BEAT);

comment('say hi — engaging the agent launches a real sandbox container');
type(`curl -s localhost:8787/v1/ui/chat/send -H 'authorization: Bearer ironclaw-demo' \\`);
cont(`-d '{"agentGroupID":"mock-agent","text":"hello from ironclaw"}'`);
out([
  '{"conversationId":"mock-agent","' + c.ok('engaged') + '":true,',
  ' "outcomes":[{"SessionID":"' + c.addr('ses_8a36aa548d04fd37') + '","Engaged":true,',
  '   "Reason":"engaged (trigger=1)"}]}',
]);
wait(BEAT);

comment('a fresh, isolated sandbox is now running for this session');
type(`docker ps --filter name=ic-sbx --format '{{.Names}}  {{.Status}}'`);
out([
  'ic-sbx-ses_8a36aa548d04fd37  Up 5 seconds   ' + c.dim('← sandboxed execution, per session'),
]);
wait(BEAT);

comment('read the reply — it flowed back through the encrypted per-session queue');
type(`curl -s localhost:8787/v1/ui/chat/mock-agent/messages \\`);
cont(`-H 'authorization: Bearer ironclaw-demo' | jq -r '.messages[0].content'`);
out([
  c.reply('mock-agent received: [chat via webchat mock-agent]'),
  c.reply('hello from ironclaw'),
]);
wait(BEAT + 0.3);

emit(c.ok('✔') + ' one command → live agent chat → real sandboxed execution. ' + c.bold('zero credentials.') + '\r\n');
wait(0.2);
emit(c.dim('  production seals each sandbox with gVisor + network=none.') + '\r\n');
wait(1.4);

// ── write cast ──────────────────────────────────────────────────────────────
const lines = [JSON.stringify(header)];
for (const e of events) lines.push(JSON.stringify(e));
process.stdout.write(lines.join('\n') + '\n');
