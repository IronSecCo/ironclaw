// IronClaw web console — T-220 scaffold.
//
// A dependency-free SPA. The single rule that keeps the security posture intact:
// every call to the control-plane goes through api(), which attaches the bearer
// token. The static shell that loads this file is auth-exempt (a browser cannot
// header a navigation), but nothing here reads data or takes an action without
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
  const el = document.getElementById("status");
  el.textContent = msg;
  el.className = "status" + (kind ? " " + kind : "");
}

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else node.setAttribute(k, v);
  }
  for (const c of children) node.append(c);
  return node;
}

// ---- Approvals -------------------------------------------------------------
// The approvals inbox is owned by approvals.js (T-221), which renders the
// read-model-backed list (/v1/ui/approvals) with approve/reject. The shell just
// delegates so connection, tabs, and the refresh button stay in one place.

function loadApprovals() {
  return Approvals.load();
}

// ---- Audit -----------------------------------------------------------------

async function loadAudit() {
  const host = document.getElementById("audit-list");
  host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
  try {
    const entries = (await api("/v1/audit?limit=50")) || [];
    if (entries.length === 0) {
      host.replaceChildren(el("p", { class: "muted", text: "No audit entries." }));
      return;
    }
    host.replaceChildren(...entries.map((e) =>
      el("pre", { class: "payload", text: JSON.stringify(e, null, 2) })));
  } catch (e) {
    host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
  }
}

// ---- Connection + tabs -----------------------------------------------------

async function connect() {
  const token = document.getElementById("token").value.trim();
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
  setStatus("connecting…");
  try {
    await api("/healthz");
    setStatus("connected", "ok");
    await loadApprovals();
  } catch (e) {
    setStatus(String(e.message || e), "error");
  }
}

function showPanel(name) {
  for (const tab of document.querySelectorAll(".tab")) {
    tab.classList.toggle("active", tab.dataset.panel === name);
  }
  for (const panel of document.querySelectorAll(".panel")) {
    const on = panel.id === name;
    panel.classList.toggle("active", on);
    panel.hidden = !on;
  }
  if (name === "audit") loadAudit();
  if (name === "approvals") loadApprovals();
}

function init() {
  document.getElementById("connect").addEventListener("click", connect);
  document.getElementById("token").addEventListener("keydown", (e) => {
    if (e.key === "Enter") connect();
  });
  document.getElementById("refresh-approvals").addEventListener("click", loadApprovals);
  document.getElementById("refresh-audit").addEventListener("click", loadAudit);
  for (const tab of document.querySelectorAll(".tab")) {
    tab.addEventListener("click", () => showPanel(tab.dataset.panel));
  }
  // A token may already be in this tab's sessionStorage from a prior reload.
  if (sessionStorage.getItem(TOKEN_KEY)) connect();
}

document.addEventListener("DOMContentLoaded", init);
