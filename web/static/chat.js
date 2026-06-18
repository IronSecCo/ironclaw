// IronClaw web console — Chat playground.
//
// Pick an agent group, send a message via POST /v1/ui/chat/send (which feeds the
// NORMAL router/delivery path) and poll GET /v1/ui/chat/{id}/messages for replies,
// rendered as bubbles. The agent picker is populated from /v1/ui/agents so you no
// longer need to know an id by hand. Shares api()/el()/setStatus() with app.js.
"use strict";

const Chat = (() => {
  const $ = (id) => document.getElementById(id);
  let timer = null;

  function avatarFor(who) {
    return who === "you" ? "U" : who === "agent" ? "✦" : "!";
  }

  function bubble(who, text, time) {
    return el("div", { class: "chat-msg chat-" + who },
      el("div", { class: "av", text: avatarFor(who) }),
      el("div", {},
        el("div", { class: "chat-meta", text: who + (time ? " · " + time : "") }),
        el("pre", { class: "chat-body", text: text })));
  }

  function append(node) {
    const host = $("chat-transcript");
    host.append(node);
    host.scrollTop = host.scrollHeight;
  }

  function conv() { return $("chat-ag").value.trim(); }

  // populate fills the agent picker, preserving the current selection.
  async function populate() {
    const sel = $("chat-ag");
    if (!sel) return;
    const current = sel.value;
    try {
      const list = await api("/v1/ui/agents");
      sel.innerHTML = "";
      sel.append(el("option", { value: "", text: list && list.length ? "Select an agent…" : "No agents — create one first" }));
      for (const a of (list || [])) sel.append(el("option", { value: a.id, text: (a.name || a.id) + "  ·  " + a.id }));
      if (current) sel.value = current;
    } catch (_) { /* leave the existing options */ }
  }

  // select switches to a specific agent (used by the Agents page "Chat" action).
  async function select(id) {
    await populate();
    const sel = $("chat-ag");
    if (sel) {
      if (![...sel.options].some((o) => o.value === id)) sel.append(el("option", { value: id, text: id }));
      sel.value = id;
    }
    $("chat-transcript").innerHTML = "";
    $("chat-status").textContent = "ready — say hello";
  }

  async function send() {
    const ag = conv();
    const text = $("chat-input").value;
    if (!ag) { setStatus("pick an agent group", "error"); $("chat-status").textContent = "pick an agent first"; return; }
    if (!text.trim()) return;
    append(bubble("you", text, new Date().toLocaleTimeString()));
    $("chat-input").value = "";
    try {
      const res = await api("/v1/ui/chat/send", {
        method: "POST",
        body: JSON.stringify({ agentGroupID: ag, text }),
      });
      $("chat-status").textContent = res && res.engaged ? "engaged — awaiting reply…" : "sent (no wiring engaged for this agent)";
      poll();
    } catch (e) {
      setStatus(String(e.message || e), "error");
      append(bubble("error", String(e.message || e)));
    }
  }

  function renderReply(m) {
    let text = m.content;
    try {
      const obj = JSON.parse(m.content);
      if (obj && typeof obj === "object") text = JSON.stringify(obj, null, 2);
    } catch (_) { /* plain text */ }
    const t = m.timestamp ? new Date(m.timestamp).toLocaleTimeString() : "";
    append(bubble("agent", text, t));
  }

  async function poll() {
    const ag = conv();
    if (!ag) return;
    try {
      const res = await api("/v1/ui/chat/" + encodeURIComponent(ag) + "/messages");
      for (const m of (res && res.messages) || []) renderReply(m);
    } catch (e) {
      setStatus(String(e.message || e), "error");
    }
  }

  function show() {
    populate();
    if (timer) return;
    timer = setInterval(() => {
      const panel = $("chat");
      if (!panel || panel.hidden) return;
      poll();
    }, 2000);
  }

  function init() {
    $("chat-send").addEventListener("click", send);
    $("chat-input").addEventListener("keydown", (e) => { if (e.key === "Enter") send(); });
    const ref = $("chat-refresh-agents");
    if (ref) ref.addEventListener("click", populate);
    const sel = $("chat-ag");
    if (sel) sel.addEventListener("change", () => {
      $("chat-transcript").innerHTML = "";
      $("chat-status").textContent = conv() ? "ready — say hello" : "pick an agent group";
    });
  }

  document.addEventListener("DOMContentLoaded", init);
  return { show, select };
})();
