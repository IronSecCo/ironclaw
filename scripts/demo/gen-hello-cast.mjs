#!/usr/bin/env node
// gen-hello-cast.mjs — deterministically build the `hello-ironclaw` example cast.
//
// A faithful re-enactment of `examples/hello-ironclaw/run.sh` (the canonical
// zero-credential end-to-end check, also the CI smoke test): one command builds the
// sandbox image, brings up the offline demo control-plane, sends a chat through the
// REAL secured path (engage -> per-session Docker sandbox -> encrypted queue ->
// delivery), and ASSERTS the reply comes back. No model key, no channel tokens.
//
// Deterministic on purpose (no Date.now / random) so the committed cast + SVG stay
// reproducible. Usage:  node scripts/demo/gen-hello-cast.mjs > docs/assets/hello.cast
//
// Honesty: the demo relaxes the sandbox to runc (shared kernel) on a default bridge
// network, so we do NOT claim network=none here. The `run.sh` output below is
// reproduced verbatim in wording (the marker pid is a fixed synthetic value).

const COLS = 92;
const ROWS = 24;

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

function type(cmd) {
  emit(c.prompt('$') + ' ');
  for (const ch of cmd) { wait(CHAR); emit(ch); }
  wait(ENTER);
  emit('\r\n');
}
function comment(text) {
  emit(c.dim('# ' + text) + '\r\n');
  wait(0.25);
}
function out(lines) {
  for (const ln of lines) { wait(LINE); emit(ln + '\r\n'); }
}
// A progress line the harness prints, revealed with a short beat + trailing dots.
function progress(text, dots = 0) {
  emit(text);
  for (let i = 0; i < dots; i++) { wait(0.12); emit('.'); }
  wait(0.18);
  emit('\r\n');
}

// ── header ────────────────────────────────────────────────────────────────
const header = {
  version: 2,
  width: COLS,
  height: ROWS,
  timestamp: 1782182997,
  idle_time_limit: 2.0,
  title: 'IronClaw — hello-ironclaw (zero-credential end-to-end)',
  env: { SHELL: '/bin/zsh', TERM: 'xterm-256color' },
  theme: {
    fg: '#eaf2ff',
    bg: '#0b1124',
    palette: [
      '#1d2c57', '#ff8087', '#5ad6a0', '#e3b341',
      '#3b82f6', '#b9a0ff', '#63a0ff', '#b9d4ff',
      '#3a4a7a', '#ff9aa0', '#7ce8bb', '#f0c460',
      '#63a0ff', '#cdbcff', '#93b6ff', '#ffffff',
    ].join(':'),
  },
};

// ── scene ──────────────────────────────────────────────────────────────────
wait(0.5);
comment('hello-ironclaw — the canonical "it works", zero credentials. One command.');
wait(0.2);

type('examples/hello-ironclaw/run.sh');
out([
  c.dim('==> building the sandbox image (ironclaw-sandbox:latest)'),
  c.dim('==> starting the offline demo control-plane (docker compose up)'),
]);
wait(BEAT);

progress(c.dim('==> waiting for the control-plane to be ready (up to 90s)'), 5);
out([
  ' ' + c.ok('✔') + ' control-plane ready at ' + c.addr('http://127.0.0.1:8787') +
    '  ' + c.dim('· offline mock-agent, zero credentials'),
]);
wait(BEAT);

comment('send a chat through the REAL secured path and assert the reply comes back');
out([c.dim('==> sending a chat message to \'mock-agent\': "hello from hello-ironclaw"')]);
progress(c.dim('==> waiting for the reply (real sandbox launch + encrypted queue round-trip)'), 3);
wait(0.2);
out([
  '    ' + c.dim('agent replied:') + ' ' +
    c.reply('mock-agent received: [chat via webchat mock-agent] hello from hello-ironclaw'),
]);
wait(BEAT + 0.3);

emit('\r\n');
emit(c.ok('PASS ✅') + '  ' + c.bold('IronClaw is working end-to-end with zero credentials.') + '\r\n');
wait(0.15);
emit(c.dim('         message → engage → sandboxed mock-agent → encrypted queue → reply.') + '\r\n');
wait(0.2);
emit(c.dim('         production seals each sandbox with gVisor + network=none.') + '\r\n');
// Hold on the completed PASS frame (Peak-End): loop length is the LAST event's
// timestamp, so a trailing wait alone is dropped. Emit a benign no-op (bare SGR
// reset) after the dwell so the final frame — also the reduced-motion still —
// lingers before the loop restarts.
wait(2.6);
emit(R);

// ── write cast ──────────────────────────────────────────────────────────────
const lines = [JSON.stringify(header)];
for (const e of events) lines.push(JSON.stringify(e));
process.stdout.write(lines.join('\n') + '\n');
