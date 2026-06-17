// IronClaw web console — Chat playground (T-226).
//
// Sends a message to a chosen agent group via POST /v1/ui/chat/send (which feeds
// the NORMAL router/delivery path) and polls GET /v1/ui/chat/{id}/messages for the
// agent's replies, rendered inline. Shares api()/el()/setStatus() with app.js.
"use strict";

const Chat = (() => {
  const $ = (id) => document.getElementById(id);
  let timer = null;

  function bubble(who, text, time) {
    return el("div", { class: "chat-msg chat-" + who },
      el("div", { class: "chat-meta", text: who + (time ? " · " + time : "") }),
      el("pre", { class: "chat-body", text: text }));
  }

  function append(node) {
    const host = $("chat-transcript");
    host.append(node);
    host.scrollTop = host.scrollHeight;
  }

  function conv() {
    return $("chat-ag").value.trim();
  }

  async function send() {
    const ag = conv();
    const text = $("chat-input").value;
    if (!ag) {
      setStatus("enter an agent group id", "error");
      return;
    }
    if (!text.trim()) return;
    append(bubble("you", text, new Date().toLocaleTimeString()));
    $("chat-input").value = "";
    try {
      const res = await api("/v1/ui/chat/send", {
        method: "POST",
        body: JSON.stringify({ agentGroupID: ag, text }),
      });
      $("chat-status").textContent = res && res.engaged ? "engaged — awaiting reply…" : "sent (no wiring engaged)";
      $("chat-status").className = "status " + (res && res.engaged ? "ok" : "");
      poll(); // pull any immediate replies
    } catch (e) {
      setStatus(String(e.message || e), "error");
      append(bubble("error", String(e.message || e)));
    }
  }

  // renderReply shows the agent message; if the content is structured (tool calls
  // / token usage as JSON), it is pretty-printed, otherwise shown as text.
  function renderReply(m) {
    let text = m.content;
    try {
      const obj = JSON.parse(m.content);
      if (obj && typeof obj === "object") text = JSON.stringify(obj, null, 2);
    } catch (_) {
      /* plain text */
    }
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
      // A poll failure is transient; surface it but keep the loop alive.
      setStatus(String(e.message || e), "error");
    }
  }

  function show() {
    // Poll only while the chat panel is visible.
    if (timer) return;
    timer = setInterval(() => {
      const panel = $("chat");
      if (!panel || panel.hidden) return;
      poll();
    }, 2000);
  }

  function init() {
    $("chat-send").addEventListener("click", send);
    $("chat-input").addEventListener("keydown", (e) => {
      if (e.key === "Enter") send();
    });
  }

  document.addEventListener("DOMContentLoaded", init);
  return { show };
})();
