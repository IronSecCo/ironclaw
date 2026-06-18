// IronClaw web console — Approvals inbox.
//
// Renders the pending-change inbox from the read-model endpoint /v1/ui/approvals
// (enriched with group/requester names) and drives approve/reject through the
// existing /v1/changes/{id}/decision endpoint. Shares api()/el()/setStatus() with
// app.js (loaded first); exposes a small global the shell calls to (re)load.
"use strict";

const Approvals = (() => {
  // fmtTime renders an ISO timestamp compactly, falling back to the raw value.
  function fmtTime(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  // diffPayload shows After (the proposed state), or a Before→After pair when both
  // are present, pretty-printed. Raw JSON is the honest, complete view for a
  // scaffold inbox; a structured field diff is a later refinement.
  function payloadText(c) {
    const pretty = (raw) => {
      if (raw == null) return "";
      try {
        return JSON.stringify(raw, null, 2);
      } catch (_) {
        return String(raw);
      }
    };
    if (c.before != null && c.after != null) {
      return "- before:\n" + pretty(c.before) + "\n\n+ after:\n" + pretty(c.after);
    }
    return pretty(c.after != null ? c.after : c.before);
  }

  function renderRow(c) {
    const card = el("article", { class: "card" });

    const group = c.agentGroupName
      ? c.agentGroupName + " (" + c.agentGroupId + ")"
      : c.agentGroupId || "—";
    const who = c.requestedByName
      ? c.requestedByName + " (" + c.requestedBy + ")"
      : c.requestedBy || "—";

    card.append(
      el("div", { class: "card-head" },
        el("span", { class: "kind", text: String(c.kind ?? "change") }),
        el("span", { class: "id", text: String(c.id ?? "") })
      ),
      el("dl", { class: "meta" },
        el("dt", { text: "Group" }), el("dd", { text: group }),
        el("dt", { text: "Requester" }), el("dd", { text: who }),
        el("dt", { text: "Requested" }), el("dd", { text: fmtTime(c.createdAt) })
      ),
      el("pre", { class: "payload", text: payloadText(c) })
    );

    const approve = el("button", { type: "button", text: "Approve" });
    const reject = el("button", { class: "danger", type: "button", text: "Reject" });
    approve.addEventListener("click", () => decide(c.id, "approve", card));
    reject.addEventListener("click", () => decide(c.id, "reject", card));
    card.append(el("div", { class: "actions" }, approve, reject));
    return card;
  }

  async function decide(id, outcome, card) {
    if (!id) return;
    card.querySelectorAll("button").forEach((b) => (b.disabled = true));
    try {
      await api("/v1/changes/" + encodeURIComponent(id) + "/decision", {
        method: "POST",
        body: JSON.stringify({ outcome, decidedBy: "console" }),
      });
      setStatus(outcome + "d " + id, "ok");
      await load(); // live update: the decided change drops off the pending list
    } catch (e) {
      setStatus(String(e.message || e), "error");
      card.querySelectorAll("button").forEach((b) => (b.disabled = false));
    }
  }

  async function load() {
    const host = document.getElementById("approvals-list");
    if (!host) return;
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const items = (await api("/v1/ui/approvals")) || [];
      // Keep the sidebar badge in sync on every load — including after a decide()
      // drops the last pending change (otherwise the badge stays stale until the
      // dashboard is revisited, since only Dashboard.load() set it before).
      if (typeof updateApprovalsBadge === "function") updateApprovalsBadge(items.length);
      if (items.length === 0) {
        host.replaceChildren(el("p", { class: "muted", text: "No pending changes." }));
        return;
      }
      host.replaceChildren(...items.map(renderRow));
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  return { load };
})();
