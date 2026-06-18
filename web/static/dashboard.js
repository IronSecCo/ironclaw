// IronClaw web console — Dashboard (overview).
//
// Read-only at-a-glance stats (agents, pending approvals, live sessions, wired
// channels) plus a preview of what's awaiting approval. Pulls the same read-models
// the feature pages use; tolerates individual endpoint failures. Shares helpers
// with app.js.
"use strict";

const Dashboard = (() => {
  const $ = (id) => document.getElementById(id);
  const set = (id, v) => { const e = $(id); if (e) e.textContent = v; };

  async function load() {
    const [agents, approvals, sessions] = await Promise.all([
      api("/v1/ui/agents").catch(() => null),
      api("/v1/ui/approvals").catch(() => null),
      api("/v1/ui/sessions").catch(() => null),
    ]);

    set("stat-agents", agents ? agents.length : "—");
    set("stat-sessions", sessions ? sessions.length : "—");
    set("stat-approvals", approvals ? approvals.length : "—");
    set("stat-channels", agents ? agents.reduce((n, a) => n + (a.destinations || 0), 0) : "—");
    if (approvals) updateApprovalsBadge(approvals.length);

    const host = $("dash-approvals");
    if (!host) return;
    host.innerHTML = "";
    if (!approvals) { host.append(el("p", { class: "muted", text: "Not connected — add your API token in the sidebar." })); return; }
    if (!approvals.length) { host.append(emptyState("All clear", "No changes are waiting for a decision.")); return; }

    for (const a of approvals.slice(0, 5)) {
      const review = el("button", { class: "ghost btn-sm", type: "button", text: "Review" });
      review.addEventListener("click", () => goPanel("approvals"));
      host.append(el("div", { class: "card" },
        el("div", { class: "card-head" },
          el("span", { class: "kind", text: a.kind }),
          el("strong", { text: a.agentGroupName || a.agentGroupId || "—" }),
          el("span", { class: "spacer" }),
          el("span", { class: "id mono", text: a.requestedByName || a.requestedBy || "" })),
        el("div", { class: "actions" }, review)));
    }
  }

  return { load };
})();
