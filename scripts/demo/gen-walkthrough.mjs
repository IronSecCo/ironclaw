#!/usr/bin/env node
// gen-walkthrough.mjs — deterministically build the IronClaw end-to-end product
// walkthrough asciinema v2 cast (docs/assets/walkthrough.cast).
//
// Where the hero demo (gen-cast.mjs) shows only the zero-credential reply, this asset
// narrates the FULL journey a new user actually travels, in three labeled acts:
//
//   ACT 1 · See it work, zero credentials   — the offline mock-agent replies, no key.
//   ACT 2 · Connect a real provider          — the one credential step; key stays host-side.
//   ACT 3 · Your first real task             — a change is HELD at the gateway, approved,
//                                              and lands in the append-only audit log.
//
// Faithful re-enactment of the documented happy path (docs/quickstart.md): the commands,
// flags, id formats (`chg_<hex>`, gateway.go), and outcomes match the real CLI. Secrets
// are synthetic and redacted (`sk-ant-••••`). Deterministic on purpose — no Date.now /
// random — so the committed cast + SVG stay reproducible.
//
//   Usage:  node scripts/demo/gen-walkthrough.mjs > docs/assets/walkthrough.cast
//
// Honesty: ACT 1 relaxes isolation (runc, shared kernel) exactly like the hero demo, so it
// does NOT claim network=none. ACT 2/3 name gVisor + network=none as the production posture
// the real control-plane enforces — it is the destination, not the demo.

const COLS = 94;
const ROWS = 30;

// ── ANSI helpers (real ESC bytes; xterm.js / svg-term renders these) ──────────
const E = '\x1b[';
const R = E + '0m';
const c = {
  prompt: (s) => E + '92m' + s + R,        // bright green  $
  ok: (s) => E + '92m' + s + R,            // success green
  warn: (s) => E + '38;5;179m' + s + R,    // amber — held / awaiting human
  addr: (s) => E + '38;5;110m' + s + R,    // steel-blue link / id
  reply: (s) => E + '38;5;153m' + s + R,   // light-blue agent reply
  dim: (s) => E + '38;5;67m' + s + R,      // muted steel-blue comment / annotation
  act: (s) => E + '1m' + E + '38;5;111m' + s + R, // bold steel-blue act banner
  redact: (s) => E + '38;5;103m' + s + R,  // greyed redacted secret
  bold: (s) => E + '1m' + s + R,
};

const events = [];
let t = 0; // seconds, monotonic

const CHAR = 0.026;   // per typed character
const ENTER = 0.34;   // pause after Enter, before output
const LINE = 0.06;    // between streamed output lines
const BEAT = 0.7;     // narrative pause between blocks
const ACTGAP = 1.0;   // pause at an act boundary

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

// A full-width act banner — Von Restorff anchor that chunks the journey.
function act(label) {
  wait(ACTGAP);
  const rule = '────────────────────────────────────────────────────────────────────────────';
  emit(c.dim(rule) + '\r\n');
  wait(0.12);
  emit('  ' + c.act(label) + '\r\n');
  emit(c.dim(rule) + '\r\n');
  wait(0.4);
}

// ── header ────────────────────────────────────────────────────────────────
const header = {
  version: 2,
  width: COLS,
  height: ROWS,
  timestamp: 1782182997, // 2026-06-23T02:49:57Z (fixed for determinism)
  idle_time_limit: 1.2,
  title: 'IronClaw — end-to-end walkthrough',
  env: { SHELL: '/bin/zsh', TERM: 'xterm-256color' },
  // IronClaw steel-blue terminal theme (travels with the cast; honored by svg-term
  // and asciinema players). Palette = ANSI 0..15 from the brand palette.
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
comment('IronClaw, end to end — from zero credentials to your first approved task.');
wait(0.2);

// ═══ ACT 1 — zero credentials ════════════════════════════════════════════════
act('ACT 1 · See it work — zero credentials');
comment('one command brings up the offline mock-agent. No API key, no signup.');
type('docker compose -f docker-compose.demo.yml up -d');
out([
  ' ' + c.ok('✔') + ' Container ironclaw-demo-controlplane-1  ' + c.ok('Started'),
  '   ' + c.addr('http://127.0.0.1:8787') + '  ' + c.dim('·  mock-agent seeded, zero credentials'),
]);
wait(BEAT);

comment('say hi — engaging the agent launches a real, isolated per-session sandbox');
type(`curl -s localhost:8787/v1/ui/chat/send -H 'authorization: Bearer ironclaw-demo' \\`);
cont(`-d '{"agentGroupID":"mock-agent","text":"hello from ironclaw"}'`);
out([
  '{"conversationId":"mock-agent","' + c.ok('engaged') + '":true,',
  ' "outcomes":[{"SessionID":"' + c.addr('ses_8a36aa548d04fd37') + '","Engaged":true}]}',
]);
wait(0.4);
type(`curl -s localhost:8787/v1/ui/chat/mock-agent/messages \\`);
cont(`-H 'authorization: Bearer ironclaw-demo' | jq -r '.messages[0].content'`);
out([
  c.reply('mock-agent received: [chat via webchat mock-agent]'),
  c.reply('hello from ironclaw'),
]);
wait(0.3);
emit(' ' + c.ok('✔') + ' It replies with ' + c.bold('no key at all') + '. Now wire a real model. \r\n');
wait(BEAT);

// ═══ ACT 2 — connect a real provider ═════════════════════════════════════════
act('ACT 2 · Connect a real provider');
comment('the ONE credential step. The key is held host-side — it never enters a sandbox.');
type('export ANTHROPIC_API_KEY=' + c.redact('sk-ant-••••••••••••••••') + '   ' + c.dim('# redacted; host-side only'));
type('export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)   ' + c.dim('# bearer for the admin API'));
wait(0.3);
comment('start the real control-plane (not the demo) — it owns the credential and the seal');
type('./bin/controlplane --api-addr 127.0.0.1:8787');
out([
  '   host/controlplane: serving on ' + c.addr('http://127.0.0.1:8787'),
  '   ' + c.dim('provider=anthropic wired host-side · each session sealed: gVisor + network=none'),
]);
wait(0.3);
emit(' ' + c.ok('✔') + ' A real model is connected. The agent still ' + c.bold('cannot reach the network or its own config') + '.\r\n');
wait(BEAT);

// ═══ ACT 3 — first real task, held for approval ══════════════════════════════
act('ACT 3 · Your first real task — held for human approval');
comment('the agent proposes a change. It does NOT self-apply — the gateway holds it.');
type('./bin/ironctl change submit --kind persona --group default --by alice');
out([
  '{"id":"' + c.addr('chg_7b3e1a9c4d20f8e1') + '","kind":"persona","status":"' + c.warn('held') + '"}',
  '   ' + c.dim('parked at the gateway — not applied until a human decides'),
]);
wait(0.4);
type('./bin/ironctl change pending');
out([
  c.addr('chg_7b3e1a9c4d20f8e1') + '  persona  default  by:alice  ' + c.warn('HELD'),
]);
wait(0.4);
comment('a human approves it — only now does it take effect');
type('./bin/ironctl change approve chg_7b3e1a9c4d20f8e1 --by alice');
out([
  '{"id":"chg_7b3e1a9c4d20f8e1","status":"' + c.ok('applied') + '","decidedBy":"alice"}',
]);
wait(0.4);
comment('every decision is on the append-only, tamper-evident audit log');
type('./bin/ironctl audit --limit 3');
out([
  '  ' + c.dim('2026-06-23T02:50') + '  ' + c.addr('chg_7b3e1a…') + '  submit    persona  by:alice',
  '  ' + c.dim('2026-06-23T02:50') + '  ' + c.addr('chg_7b3e1a…') + '  decision  ' + c.ok('approved') + '  by:alice',
  '  ' + c.dim('2026-06-23T02:50') + '  ' + c.addr('chg_7b3e1a…') + '  ' + c.ok('applied') + '   persona  by:alice',
]);
wait(BEAT);

emit(c.ok('✔') + ' zero-cred demo → real provider → first approved task. ' + c.bold('Isolation you can prove.') + '\r\n');
wait(0.2);
emit(c.dim('  Full quickstart: docs/quickstart.md  ·  the gateway is the choke point, by design.') + '\r\n');
wait(1.5);

// ── write cast ──────────────────────────────────────────────────────────────
const lines = [JSON.stringify(header)];
for (const e of events) lines.push(JSON.stringify(e));
process.stdout.write(lines.join('\n') + '\n');
