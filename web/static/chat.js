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
  // Liveness tracking (IRO-128): once a send engages we await a reply. Without
  // this the transcript shows nothing whether the agent is working, still
  // thinking, or has silently failed — indistinguishable bare empty state. We
  // surface elapsed "working…" feedback and a staleness hint with a concrete
  // diagnostic if no reply lands within STALE_AFTER_MS.
  const STALE_AFTER_MS = 30000;
  let awaitingSince = 0; // ms epoch of the last engaged send still awaiting a reply
  let staleNoted = false; // ensures the staleness diagnostic is appended once

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
    // The empty state is purely a before-first-message orientation cue: the
    // moment any real card (a "you"/agent bubble or the error diagnostic) lands
    // it must yield, so clear it on every append (IRO-218).
    const placeholder = host.querySelector(".empty");
    if (placeholder) placeholder.remove();
    host.append(node);
    host.scrollTop = host.scrollHeight;
  }

  function conv() { return $("chat-ag").value.trim(); }

  // renderEmpty draws a centered orientation cue in the otherwise-blank
  // conversation region before the first message (IRO-218). It reuses the
  // shared emptyState() pattern (muted icon + title + one line) so the Chat
  // playground reads like the Agents/Sessions tabs. It is a no-op once any real
  // message bubble exists, and append() removes it the instant one appears.
  function renderEmpty() {
    const host = $("chat-transcript");
    if (!host) return;
    const existing = host.querySelector(".empty");
    if (existing) existing.remove();
    if (host.querySelector(".chat-msg")) return; // real transcript present — no cue
    const sel = $("chat-ag");
    const ag = sel ? sel.value.trim() : "";
    let box;
    if (!ag) {
      // No agent picked yet — point attention at the selector above.
      box = emptyState("Pick an agent to start", "", null, null, EMPTY_ICONS.agents);
      box.querySelector("p").innerHTML =
        "Choose an agent group above — the demo ships with <strong>Mock Agent (offline)</strong>, " +
        "which runs the full engage → sandbox → reply path with no API key.";
    } else {
      // Agent picked, transcript still blank — invite the first message.
      const opt = sel.selectedOptions && sel.selectedOptions[0];
      const name = (opt ? opt.text : ag).split("·")[0].trim() || ag;
      box = emptyState("Say hi to " + name,
        "Your message runs the real engage / delivery path. First reply can take a few seconds while the sandbox spins up.",
        null, null, EMPTY_ICONS.channels);
    }
    host.append(box);
  }

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
    renderEmpty(); // refresh the before-first-message orientation cue (IRO-218)
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
    awaitingSince = 0;
    staleNoted = false;
    renderEmpty();
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
      if (res && res.engaged) {
        awaitingSince = Date.now();
        staleNoted = false;
        $("chat-status").textContent = "agent is working…";
      } else {
        awaitingSince = 0;
        $("chat-status").textContent = "sent (no wiring engaged for this agent)";
      }
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
      const msgs = (res && res.messages) || [];
      for (const m of msgs) renderReply(m);
      if (msgs.length) {
        // A reply landed — the agent is alive. Stop awaiting.
        awaitingSince = 0;
        staleNoted = false;
        $("chat-status").textContent = "reply received";
      } else if (awaitingSince) {
        liveness();
      }
    } catch (e) {
      setStatus(String(e.message || e), "error");
    }
  }

  // liveness updates the status line while awaiting a reply: elapsed "working…"
  // feedback, then a one-time staleness diagnostic if nothing lands within
  // STALE_AFTER_MS so a silent failure is distinguishable from "still thinking".
  function liveness() {
    const secs = Math.round((Date.now() - awaitingSince) / 1000);
    if (Date.now() - awaitingSince >= STALE_AFTER_MS) {
      $("chat-status").textContent = "no reply after " + secs + "s — the agent may have stalled";
      if (!staleNoted) {
        staleNoted = true;
        append(bubble("error",
          "No reply after " + secs + "s. The agent engaged but has not replied — it may still be thinking, " +
          "or it may have silently failed.\n\nTo diagnose, on the host find the sandbox container and read its logs:\n" +
          "  docker ps --filter name=ic-sbx-\n" +
          "  docker logs --tail 50 <container>\n" +
          "(look for `engaging N message(s)` and `wrote reply …`; an `engaging` line with no following " +
          "`wrote reply` means the turn produced no output — `model produced no reply text` confirms it). " +
          "You can also run `ironctl doctor` to verify the sandbox image, mounts, and model-proxy socket."));
      }
    } else {
      $("chat-status").textContent = "agent is working… (" + secs + "s)";
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
      awaitingSince = 0;
      staleNoted = false;
      renderEmpty();
    });
  }

  document.addEventListener("DOMContentLoaded", init);
  return { show, select };
})();
