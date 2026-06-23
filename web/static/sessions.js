// IronClaw web console — Sessions browser.
//
// Lists live sessions from the read-model /v1/ui/sessions (enriched with the
// agent-group name) with an inline detail view, and terminates a sandbox via
// POST /v1/ui/sessions/{id}/terminate (the only host-control action the console
// has). Shares api()/el()/setStatus() with app.js.
"use strict";

const Sessions = (() => {
  function fmtTime(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  function statusClass(status) {
    const s = String(status || "").toLowerCase();
    if (s === "running") return "ok";
    if (s === "stopped" || s === "failed") return "error";
    return "";
  }

  function renderRow(sess) {
    const card = el("article", { class: "card" });
    const group = sess.agentGroupName
      ? sess.agentGroupName + " (" + sess.agentGroupId + ")"
      : sess.agentGroupId || "—";

    card.append(
      el("div", { class: "card-head" },
        el("span", { class: "kind " + statusClass(sess.containerStatus), text: String(sess.containerStatus || "unknown") }),
        el("span", { class: "id", text: String(sess.id ?? "") })
      ),
      el("dl", { class: "meta" },
        el("dt", { text: "Agent group" }), el("dd", { text: group }),
        el("dt", { text: "Messaging group" }), el("dd", { text: sess.messagingGroupId || "—" }),
        el("dt", { text: "Last active" }), el("dd", { text: fmtTime(sess.lastActive) })
      )
    );

    const terminate = el("button", { class: "danger", type: "button", text: "Terminate" });
    terminate.addEventListener("click", () => doTerminate(sess.id, card));
    card.append(el("div", { class: "actions" }, terminate));
    return card;
  }

  async function doTerminate(id, card) {
    if (!id) return;
    if (!window.confirm("Terminate session " + id + "? The sandbox will be stopped.")) return;
    card.querySelectorAll("button").forEach((b) => (b.disabled = true));
    try {
      await api("/v1/ui/sessions/" + encodeURIComponent(id) + "/terminate", { method: "POST" });
      setStatus("terminated " + id, "ok");
      await load(); // live update: the stopped session refreshes its status
    } catch (e) {
      setStatus(String(e.message || e), "error");
      card.querySelectorAll("button").forEach((b) => (b.disabled = false));
    }
  }

  async function load() {
    const host = document.getElementById("sessions-list");
    if (!host) return;
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const items = (await api("/v1/ui/sessions")) || [];
      if (items.length === 0) {
        host.replaceChildren(emptyState("No sessions yet",
          "A session — a live, isolated sandbox — starts the moment an agent first replies. Create an agent and open Chat to begin.",
          "Open Chat", "chat", EMPTY_ICONS.sessions));
        return;
      }
      host.replaceChildren(...items.map(renderRow));
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  return { load };
})();
