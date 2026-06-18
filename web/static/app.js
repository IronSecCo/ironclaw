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

// emptyState renders a friendly placeholder with an optional call to action.
function emptyState(title, sub, ctaLabel, ctaPanel) {
  const box = el("div", { class: "empty" },
    el("h3", { text: title }),
    el("p", { class: "muted", text: sub || "" }));
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

// boot probes liveness, sets the connection status, and loads the visible panel.
// It does NOT touch the stored token, so it is safe to run on every page load.
async function boot(announce) {
  try {
    await api("/healthz");
    setStatus("connected", "ok");
    if (announce) toast("Connected", "ok");
  } catch (e) {
    setStatus(String(e.message || e), "error");
    if (announce) toast(String(e.message || e), "error");
  }
  refreshPanel(activePanel());
  if (typeof Dashboard !== "undefined") Dashboard.load();
  refreshAgents();
  refreshMgGroups();
}

// connect stores the token from the input, then boots.
function connect() {
  const token = document.getElementById("token").value.trim();
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
  setStatus("connecting…");
  return boot(true);
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
  document.getElementById("connect").addEventListener("click", connect);
  document.getElementById("token").addEventListener("keydown", (e) => { if (e.key === "Enter") connect(); });
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
  if (stored) document.getElementById("token").value = stored;
  boot(false);
}

document.addEventListener("DOMContentLoaded", init);
