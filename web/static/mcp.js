// IronClaw web console — MCP servers.
//
// Connect Model Context Protocol servers (local stdio or remote HTTP), discover their
// tools, and grant an agent a NAMED subset. Server CRUD is operator infrastructure (a
// direct registry write); GRANTING an agent goes through POST /v1/ui/config/change with
// kind mcp_access, so it lands on the gateway's human-approval floor (Approvals). The
// sandbox never speaks MCP — the host broker is the only thing that does. Shares
// api()/el()/toast()/fillAgentSelects() with app.js. When the daemon has no MCP catalog
// the endpoints return 503 and this shows a hint.
"use strict";

const MCP = (() => {
  const $ = (id) => document.getElementById(id);

  // splitArgs splits a command-arguments string on whitespace (empty -> []).
  function splitArgs(s) {
    return (s || "").split(/\s+/).map((x) => x.trim()).filter(Boolean);
  }
  // parseKV parses "K=V, K2=V2" into an object (empty -> undefined).
  function parseKV(s) {
    const out = {};
    let any = false;
    for (const pair of (s || "").split(",")) {
      const i = pair.indexOf("=");
      if (i <= 0) continue;
      out[pair.slice(0, i).trim()] = pair.slice(i + 1).trim();
      any = true;
    }
    return any ? out : undefined;
  }

  // toggleTransport shows the stdio or http fields based on the Type select.
  function toggleTransport() {
    const http = $("mcp-transport").value === "http";
    $("mcp-http-fields").hidden = !http;
    $("mcp-stdio-fields").hidden = http;
    $("mcp-stdio-fields2").hidden = http;
  }

  async function save() {
    const name = $("mcp-name").value.trim();
    if (!name) { toast("a server name is required", "error"); return; }
    const transport = $("mcp-transport").value;
    const cfg = { transport };
    if (transport === "stdio") {
      cfg.command = $("mcp-command").value.trim();
      cfg.args = splitArgs($("mcp-args").value);
      const image = $("mcp-image").value.trim(); if (image) cfg.image = image;
      const env = parseKV($("mcp-env").value); if (env) cfg.env = env;
      if (!cfg.command) { toast("a local server needs a command", "error"); return; }
    } else {
      cfg.url = $("mcp-url").value.trim();
      const auth = $("mcp-auth").value.trim(); if (auth) cfg.headers = { Authorization: auth };
      if (!cfg.url) { toast("a remote server needs a URL", "error"); return; }
    }
    try {
      await api("/v1/registry/mcp-servers/" + encodeURIComponent(name), { method: "PUT", body: JSON.stringify(cfg) });
      toast("saved server " + name, "ok");
      ["mcp-name", "mcp-command", "mcp-args", "mcp-image", "mcp-env", "mcp-url", "mcp-auth"].forEach((id) => { const e = $(id); if (e) e.value = ""; });
      load();
    } catch (e) { toast(String(e.message || e), "error"); }
  }

  async function del(name) {
    if (!name) return;
    try {
      await api("/v1/registry/mcp-servers/" + encodeURIComponent(name), { method: "DELETE" });
      toast("removed " + name, "ok");
      load();
    } catch (e) { toast(String(e.message || e), "error"); }
  }

  // grant submits an mcp_access capability change for human approval. tools=[] means
  // "all the server's tools".
  async function grant(server, agentId, by, tools) {
    if (!agentId) { toast("pick an agent to grant", "error"); return; }
    const body = JSON.stringify({
      kind: "mcp_access",
      agentGroupID: agentId,
      requestedBy: by || "console",
      after: { server, tools },
    });
    try {
      const res = await api("/v1/ui/config/change", { method: "POST", body });
      toast("grant proposed" + (res && res.id ? " (change " + res.id + ")" : "") + " — awaits approval", "ok");
    } catch (e) { toast(String(e.message || e), "error"); }
  }

  // renderGrantForm fills a card's expandable area with discovered tools + an agent
  // picker, after a probe.
  function renderGrantForm(box, server, tools) {
    box.replaceChildren();
    if (!tools.length) {
      box.append(el("p", { class: "muted", text: "The server declared no tools." }));
      return;
    }
    const checks = el("div", { class: "form-row", style: "flex-wrap:wrap;gap:.4rem" });
    for (const t of tools) {
      const id = "mcptool-" + server + "-" + t.name;
      checks.append(el("label", { class: "muted", style: "display:flex;align-items:center;gap:.3rem;margin-right:.6rem", title: t.description || "" },
        el("input", { type: "checkbox", value: t.name, id }), t.name));
    }
    const agentSel = el("select", { "data-agents": "" }, el("option", { value: "", text: "Select an agent…" }));
    const by = el("input", { placeholder: "requested by (e.g. slack:alice)" });
    const btn = el("button", { class: "btn-primary", type: "button" }, "Request grant (needs approval)");
    btn.addEventListener("click", () => {
      const picked = [...checks.querySelectorAll("input:checked")].map((c) => c.value);
      grant(server, agentSel.value, by.value.trim(), picked);
    });
    box.append(
      el("p", { class: "muted", text: "Pick the tools to grant (none checked = all of them), then the agent." }),
      checks,
      el("div", { class: "form-row" },
        el("div", { class: "field" }, el("label", { text: "For which agent" }), agentSel),
        el("div", { class: "field" }, el("label", { text: "Requested by" }), by),
        btn));
    // Fill the freshly-added agent picker from the cached agent list.
    if (typeof fillAgentSelects === "function") fillAgentSelects();
  }

  async function probe(server, box) {
    box.replaceChildren(el("p", { class: "muted", text: "Connecting to " + server + "…" }));
    try {
      const res = await api("/v1/registry/mcp-servers/" + encodeURIComponent(server) + "/probe", { method: "POST" });
      renderGrantForm(box, server, (res && res.tools) || []);
    } catch (e) {
      box.replaceChildren(el("p", { class: "error", text: "Probe failed: " + String(e.message || e) }));
    }
  }

  function serverTitle(s) {
    const kind = s.transport === "http" ? "remote" : "local";
    return (s.name || "") + "  ·  " + kind;
  }

  function renderCard(view) {
    const s = view.server || {};
    const grants = view.grants || [];
    const detail = s.transport === "http"
      ? (s.url || "")
      : ([s.command].concat(s.args || []).join(" "));

    const grantList = grants.length
      ? el("dd", { text: grants.map((g) => (g.agentGroupName || g.agentGroupId) + " → " + ((g.tools && g.tools.length) ? g.tools.join(", ") : "all tools")).join("  ·  ") })
      : el("dd", { class: "muted", text: "no agents granted yet" });

    const grantBox = el("div", { class: "mcp-grant" });
    const discover = el("button", { class: "ghost", type: "button" }, "Discover tools & grant");
    discover.addEventListener("click", () => probe(s.name, grantBox));
    const remove = el("button", { class: "ghost", type: "button" }, "Remove");
    remove.addEventListener("click", () => del(s.name));

    return el("article", { class: "card" },
      el("div", { class: "card-head" },
        el("span", { class: "id", text: serverTitle(s) })),
      el("dl", { class: "meta" },
        el("dt", { text: s.transport === "http" ? "endpoint" : "command" }), el("dd", { text: detail || "—" }),
        el("dt", { text: "granted to" }), grantList),
      el("div", { class: "form-row" }, discover, remove),
      grantBox);
  }

  async function load() {
    const list = $("mcp-list");
    try {
      const servers = await api("/v1/ui/mcp-servers");
      list.replaceChildren();
      if (!servers || servers.length === 0) {
        list.append(el("p", { class: "muted", text: "No MCP servers configured yet. Add one above." }));
        return;
      }
      for (const v of servers) list.append(renderCard(v));
    } catch (e) {
      const msg = String(e.message || e);
      list.replaceChildren(el("p", { class: "muted",
        text: msg.indexOf("503") === 0
          ? "MCP is not enabled on this control-plane (start the daemon with --mcp-catalog, or use --dev)."
          : "Could not load MCP servers: " + msg }));
    }
  }

  let wired = false;
  function show() {
    if (!wired) {
      $("mcp-save").addEventListener("click", save);
      $("refresh-mcp").addEventListener("click", load);
      $("mcp-transport").addEventListener("change", toggleTransport);
      toggleTransport();
      wired = true;
    }
    load();
  }

  return { show, load };
})();
