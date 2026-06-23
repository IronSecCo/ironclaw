// IronClaw web console — shell (nav, connection, shared helpers).
//
// Dependency-free SPA. The one rule that keeps the security posture intact:
// every call to the control-plane goes through api(), which attaches the bearer
// token. The static shell is auth-exempt; nothing here reads data or acts without
// the token-gated /v1 API.
"use strict";

const TOKEN_KEY = "ironclaw.token";

// api wraps fetch with the bearer token (when set) and JSON handling. Throws on a
// non-2xx so callers render a clear error instead of silently showing nothing.
async function api(path, opts = {}) {
  const headers = Object.assign({ Accept: "application/json" }, opts.headers || {});
  const token = sessionStorage.getItem(TOKEN_KEY);
  if (token) headers.Authorization = "Bearer " + token;
  if (opts.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";

  const res = await fetch(path, Object.assign({}, opts, { headers }));
  if (res.status === 401) throw new Error("401 unauthorized — check the API token");
  if (!res.ok) throw new Error(res.status + " " + res.statusText);
  const text = await res.text();
  return text ? JSON.parse(text) : null;
}

function setStatus(msg, kind) {
  const s = document.getElementById("status");
  if (s) s.textContent = msg;
  const conn = document.getElementById("conn");
  if (conn) conn.className = "conn" + (kind ? " " + kind : "");
}

// authProbe hits a bearer-gated read endpoint and reports the raw status so the
// caller can tell "connected" (200) apart from "needs a token" (401) and
// "daemon unreachable" (network error). This is what backs the real connection
// indicator — never an unauthenticated /healthz ping.
async function authProbe() {
  const token = sessionStorage.getItem(TOKEN_KEY);
  const headers = { Accept: "application/json" };
  if (token) headers.Authorization = "Bearer " + token;
  const res = await fetch("/v1/ui/agents", { headers });
  return { status: res.status, hasToken: !!token };
}

let toastTimer = null;
function toast(msg, kind) {
  const t = document.getElementById("toast");
  if (!t) return;
  t.textContent = msg;
  t.className = "toast show" + (kind ? " " + kind : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = "toast" + (kind ? " " + kind : ""); }, 2600);
}

// el builds a DOM node: el("div", {class:"x"}, child, "text").
function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else if (k === "html") node.innerHTML = v;
    else node.setAttribute(k, v);
  }
  for (const c of children) node.append(c);
  return node;
}

// Inline SVG glyphs for empty states — stroke-based, single color (currentColor),
// so they inherit .empty .ico's faint tone and stay crisp at any size. Kept here
// so every "nothing here yet" surface draws from one consistent set.
const EMPTY_ICONS = {
  sessions:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="14" rx="2"/><path d="M8 9l3 3-3 3M13 15h4"/></svg>',
  audit:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6"/></svg>',
  approvals: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l7 3v5c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6z"/><path d="M9 12l2 2 4-4"/></svg>',
  channels:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H8l-4 4V5a2 2 0 0 1 2-2h13a2 2 0 0 1 2 2z"/></svg>',
  skills:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2l2.4 4.9 5.4.8-3.9 3.8.9 5.4-4.8-2.5-4.8 2.5.9-5.4L4.2 7.7l5.4-.8z"/></svg>',
  mcp:       '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><path d="M17.5 14v3.5H21"/></svg>',
  agents:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8" r="3.2"/><path d="M5 20c0-3.3 3.1-6 7-6s7 2.7 7 6"/></svg>',
  offline:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5a7 7 0 0 1 14 0M8.5 16a3.5 3.5 0 0 1 7 0M2 9a11 11 0 0 1 20 0"/><path d="M3 3l18 18"/></svg>',
};

// emptyState renders a friendly placeholder: an optional icon, a title, a one-line
// scent, and an optional call to action. icon is an inline SVG string (e.g.
// EMPTY_ICONS.sessions) — pass it so list/grid surfaces read as deliberate empty
// states, not bare muted text.
function emptyState(title, sub, ctaLabel, ctaPanel, icon) {
  const box = el("div", { class: "empty" });
  if (icon) box.append(el("div", { class: "ico", html: icon }));
  box.append(el("h3", { text: title }));
  box.append(el("p", { class: "muted", text: sub || "" }));
  if (ctaLabel) {
    const b = el("button", { class: "btn-primary", type: "button", text: ctaLabel });
    b.addEventListener("click", () => goPanel(ctaPanel));
    box.append(b);
  }
  return box;
}

// ---- Agent pickers ---------------------------------------------------------
// Any <select data-agents> is filled from /v1/ui/agents so an operator picks an
// agent from a list instead of typing an id. Cached so panel switches are instant.
let agentCache = [];
async function refreshAgents() {
  try { agentCache = (await api("/v1/ui/agents")) || []; } catch (_) { /* keep last */ }
  fillAgentSelects();
  return agentCache;
}
function fillAgentSelects() {
  for (const sel of document.querySelectorAll("select[data-agents]")) {
    const cur = sel.value;
    sel.innerHTML = "";
    sel.append(el("option", { value: "", text: agentCache.length ? "Select an agent…" : "No agents yet — create one" }));
    for (const a of agentCache) sel.append(el("option", { value: a.id, text: a.name || a.id }));
    if (cur) sel.value = cur;
  }
}
function selectAgentIn(sel, id) {
  if (!sel) return;
  if (![...sel.options].some((o) => o.value === id)) sel.append(el("option", { value: id, text: id }));
  sel.value = id;
}

// Same pattern for messaging groups (connected chat surfaces): any
// <select data-mgs> is filled from /v1/ui/messaging-groups.
let mgCache = [];
async function refreshMgGroups() {
  try { mgCache = (await api("/v1/ui/messaging-groups")) || []; } catch (_) { /* keep last */ }
  fillMgSelects();
  return mgCache;
}
function mgLabel(m) {
  const p = m.channelType ? m.channelType.charAt(0).toUpperCase() + m.channelType.slice(1) : "Channel";
  return p + " · " + (m.platformId || m.id);
}
function fillMgSelects() {
  for (const sel of document.querySelectorAll("select[data-mgs]")) {
    const cur = sel.value;
    sel.innerHTML = "";
    sel.append(el("option", { value: "", text: mgCache.length ? "Select a chat surface…" : "Connect a surface in step 1" }));
    for (const m of mgCache) sel.append(el("option", { value: m.id, text: mgLabel(m) }));
    if (cur) sel.value = cur;
  }
}
function selectMgIn(sel, id) {
  if (!sel) return;
  if (![...sel.options].some((o) => o.value === id)) sel.append(el("option", { value: id, text: id }));
  sel.value = id;
}

// ---- Delegated feature pages (each owns its module) ------------------------
function loadApprovals() { return Approvals.load(); }
function loadAudit() { return Audit.load(); }

// ---- Connection + navigation ----------------------------------------------
function activePanel() {
  const p = document.querySelector(".panel.active");
  return p ? p.id : "dashboard";
}

// lastAuthState is the most recent result of boot(): "authed" | "open" |
// "unauth" | "offline" | "error". connect() reads it to decide whether to show
// an inline token error.
let lastAuthState = "idle";

// boot resolves the *real* connection state from an authenticated probe, sets the
// indicator accordingly, reveals/hides the first-run prompt, and loads the visible
// panel. It does NOT touch the stored token, so it is safe to run on every load.
async function boot(announce) {
  let live = true;
  try {
    const h = await fetch("/healthz");
    live = h.ok;
  } catch (_) {
    live = false;
  }
  if (!live) {
    lastAuthState = "offline";
    setStatus("offline — daemon unreachable", "error");
    if (announce) toast("Can't reach the control plane", "error");
    applyAuthState();
    return lastAuthState;
  }

  try {
    const p = await authProbe();
    if (p.status === 200) {
      lastAuthState = p.hasToken ? "authed" : "open";
      setStatus(p.hasToken ? "connected" : "connected · open API", "ok");
      if (announce) toast("Connected", "ok");
    } else if (p.status === 401) {
      lastAuthState = "unauth";
      setStatus(p.hasToken ? "token rejected (401)" : "not connected — token required", "warn");
      if (announce) toast(p.hasToken ? "Token rejected (401)" : "Add an API token to connect", "error");
    } else {
      lastAuthState = "error";
      setStatus("error · HTTP " + p.status, "error");
      if (announce) toast("Unexpected response (HTTP " + p.status + ")", "error");
    }
  } catch (e) {
    lastAuthState = "offline";
    setStatus(String(e.message || e), "error");
    if (announce) toast(String(e.message || e), "error");
  }

  applyAuthState();
  refreshPanel(activePanel());
  if (typeof Dashboard !== "undefined") Dashboard.load();
  refreshAgents();
  refreshMgGroups();
  return lastAuthState;
}

// applyAuthState shows the first-run connect prompt and opens the sidebar token
// control only when a token is actually needed (gated API, no/invalid token).
function applyAuthState() {
  const needsToken = lastAuthState === "unauth";
  const fr = document.getElementById("firstrun");
  if (fr) fr.hidden = !needsToken;
  if (needsToken) {
    const det = document.querySelector(".conn-token");
    if (det) det.open = true;
  } else {
    clearTokenError();
  }
}

function setTokenError(msg) {
  for (const id of ["token-err", "token-fr-err"]) {
    const e = document.getElementById(id);
    if (e) { e.textContent = msg; e.hidden = !msg; }
  }
}
function clearTokenError() { setTokenError(""); }

// connect stores the token from whichever input the user used, boots, and on a
// gated/rejected result shows an inline error instead of silently "succeeding".
async function connect(srcId) {
  const inp = document.getElementById(srcId || "token");
  const token = inp ? inp.value.trim() : "";
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
  clearTokenError();
  setStatus("connecting…");
  const state = await boot(true);
  if (state === "unauth") {
    setTokenError(token
      ? "Token rejected — check it matches the daemon's IRONCLAW_API_TOKEN."
      : "Enter a bearer token — this API requires authentication.");
  } else if (state === "offline") {
    setTokenError("Can't reach the control plane — is the daemon running?");
  } else {
    // Connected: keep both token inputs in sync so the value is visible wherever
    // the operator looks next.
    for (const id of ["token", "token-fr"]) {
      const el2 = document.getElementById(id);
      if (el2 && el2 !== inp) el2.value = token;
    }
  }
  return state;
}

function refreshPanel(name) {
  if (name === "dashboard" && typeof Dashboard !== "undefined") Dashboard.load();
  if (name === "agents" && typeof Agents !== "undefined") Agents.load();
  if (name === "audit") loadAudit();
  if (name === "approvals") loadApprovals();
  if (name === "sessions") Sessions.load();
  if (name === "channels") Channels.show();
  if (name === "setup") Setup.show();
  if (name === "chat") Chat.show();
  if (name === "skills") Skills.show();
  if (name === "mcp" && typeof MCP !== "undefined") MCP.show();
  if (name === "channels" || name === "setup" || name === "skills" || name === "mcp") refreshAgents();
  if (name === "channels") refreshMgGroups();
}

function showPanel(name) {
  for (const tab of document.querySelectorAll(".nav-item")) {
    tab.classList.toggle("active", tab.dataset.panel === name);
  }
  for (const panel of document.querySelectorAll(".panel")) {
    const on = panel.id === name;
    panel.classList.toggle("active", on);
    panel.hidden = !on;
  }
  refreshPanel(name);
}

// goPanel switches tabs programmatically (used by cross-page CTAs).
function goPanel(name) { if (name) showPanel(name); }

// Cross-page helpers used by the Agents cards.
function openChatWith(agentId) {
  showPanel("chat");
  if (typeof Chat !== "undefined" && Chat.select) Chat.select(agentId);
}
async function openConfigFor(agentId) {
  showPanel("setup");
  await refreshAgents();
  const inp = document.getElementById("setup-ag");
  if (inp) {
    inp.closest("details").open = true;
    selectAgentIn(inp, agentId);
    document.getElementById("setup-ag-load").click();
  }
}

// updateApprovalsBadge reflects the pending count in the sidebar.
function updateApprovalsBadge(n) {
  const b = document.getElementById("nav-approvals-count");
  if (b) b.textContent = n > 0 ? String(n) : "";
}

function init() {
  document.getElementById("connect").addEventListener("click", () => connect("token"));
  document.getElementById("token").addEventListener("keydown", (e) => { if (e.key === "Enter") connect("token"); });
  // First-run connect prompt (dashboard body; also the only token entry on mobile,
  // where the sidebar footer is hidden).
  const cfr = document.getElementById("connect-fr");
  if (cfr) cfr.addEventListener("click", () => connect("token-fr"));
  const tfr = document.getElementById("token-fr");
  if (tfr) tfr.addEventListener("keydown", (e) => { if (e.key === "Enter") connect("token-fr"); });
  const ra = document.getElementById("refresh-approvals");
  if (ra) ra.addEventListener("click", loadApprovals);
  const rau = document.getElementById("refresh-audit");
  if (rau) rau.addEventListener("click", loadAudit);
  const rs = document.getElementById("refresh-sessions");
  if (rs) rs.addEventListener("click", () => Sessions.load());

  for (const tab of document.querySelectorAll(".nav-item")) {
    tab.addEventListener("click", () => showPanel(tab.dataset.panel));
  }
  for (const b of document.querySelectorAll("[data-go]")) {
    b.addEventListener("click", () => goPanel(b.dataset.go));
  }

  // Prefill any token already held in this tab so connect() preserves it, then
  // load the visible panel immediately (works on an open loopback; a gated
  // deployment shows a 401 prompting the user to add a token in the sidebar).
  const stored = sessionStorage.getItem(TOKEN_KEY);
  if (stored) {
    for (const id of ["token", "token-fr"]) {
      const el2 = document.getElementById(id);
      if (el2) el2.value = stored;
    }
  }
  boot(false);
}

document.addEventListener("DOMContentLoaded", init);
