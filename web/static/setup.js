// IronClaw web console — Setup wizard + config editor (T-225).
//
// Two surfaces:
//  1. First-run wizard — mirrors `ironctl onboard` by rendering the dry-run step
//     statuses from GET /v1/ui/onboard (read-only; no token ever crosses).
//  2. Config editor — loads an agent group's identity + applied capability
//     history (GET /v1/ui/config/{id}); capability edits are submitted via
//     POST /v1/ui/config/change, which routes through the gateway's human-
//     approval floor (they appear in the Approvals tab). NEVER a direct write.
// Shares api()/el()/setStatus() with app.js.
"use strict";

const Setup = (() => {
  const $ = (id) => document.getElementById(id);

  function statusClass(s) {
    switch (String(s)) {
      case "ok":
      case "skipped":
        return "ok";
      case "failed":
        return "error";
      default:
        return ""; // planned / action
    }
  }

  // --- onboarding wizard --------------------------------------------------

  async function loadOnboard() {
    const host = $("setup-onboard");
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const res = await api("/v1/ui/onboard");
      const steps = (res && res.steps) || [];
      if (steps.length === 0) {
        host.replaceChildren(el("p", { class: "muted", text: "No onboarding steps reported." }));
        return;
      }
      host.replaceChildren(...steps.map((s) =>
        el("div", { class: "card" },
          el("div", { class: "card-head" },
            el("span", { class: "kind " + statusClass(s.status), text: String(s.status) }),
            el("span", { class: "id", text: s.name })),
          el("p", { text: s.detail || "" }))));
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // --- config editor ------------------------------------------------------

  async function loadConfig(id) {
    const host = $("setup-config-view");
    if (!id) {
      host.replaceChildren(el("p", { class: "muted", text: "Enter an agent group id." }));
      return;
    }
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const view = await api("/v1/ui/config/" + encodeURIComponent(id));
      const g = view.agentGroup || {};
      const applied = view.appliedChanges || [];
      const head = el("dl", { class: "meta" },
        el("dt", { text: "id" }), el("dd", { text: g.ID || id }),
        el("dt", { text: "name" }), el("dd", { text: g.Name || "—" }),
        el("dt", { text: "folder" }), el("dd", { text: g.Folder || "—" }),
        el("dt", { text: "provider/model" }), el("dd", { text: (g.Provider || "default") + " / " + (g.Model || "default") }));
      const changes = applied.length === 0
        ? el("p", { class: "muted", text: "No applied capability changes recorded." })
        : el("pre", { class: "payload", text: JSON.stringify(applied, null, 2) });
      host.replaceChildren(head, el("h3", { text: "Applied capability changes" }), changes);
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // KINDS maps a capability change kind to the fields it needs and a builder that
  // produces the gateway ChangeRequest "after" payload (per the contract's
  // per-kind shapes).
  const KINDS = {
    persona: {
      fields: [{ id: "instructions", label: "instructions", area: true }],
      build: (v) => ({ instructions: v.instructions }),
      ok: (v) => v.instructions.trim() !== "",
    },
    enabled_tools: {
      fields: [{ id: "tools", label: "tools (comma-separated)" }],
      build: (v) => splitList(v.tools),
      ok: (v) => splitList(v.tools).length > 0,
    },
    packages: {
      fields: [{ id: "packages", label: "packages (comma-separated)" }],
      build: (v) => splitList(v.packages),
      ok: (v) => splitList(v.packages).length > 0,
    },
    permissions: {
      fields: [{ id: "grant", label: "grant (role)" }, { id: "member", label: "member (user id)" }],
      build: (v) => ({ grant: v.grant.trim(), member: v.member.trim() }),
      ok: (v) => v.grant.trim() !== "" && v.member.trim() !== "",
    },
    mounts: {
      fields: [{ id: "sources", label: "mount sources (comma-separated abs paths)" }],
      build: (v) => splitList(v.sources).map((s) => ({ source: s })),
      ok: (v) => splitList(v.sources).length > 0,
    },
    wiring: {
      fields: [{ id: "engage", label: "engage (pattern/mention/mention-sticky)" }, { id: "pattern", label: "pattern (regex)" }],
      build: (v) => ({ engage: v.engage.trim(), pattern: v.pattern.trim() }),
      ok: (v) => v.engage.trim() !== "",
    },
  };

  function splitList(s) {
    return String(s || "").split(",").map((x) => x.trim()).filter(Boolean);
  }

  function renderForm() {
    const host = $("setup-change-form");
    const select = el("select", { id: "setup-kind", title: "capability change kind" });
    for (const k of Object.keys(KINDS)) select.append(el("option", { value: k, text: k }));
    const fieldsBox = el("div", { id: "setup-kind-fields" });
    const submit = el("button", { type: "button", text: "Submit change (→ gateway approval)" });

    function drawFields() {
      const spec = KINDS[select.value];
      fieldsBox.replaceChildren(...spec.fields.map((f) =>
        f.area
          ? el("textarea", { id: "setup-f-" + f.id, placeholder: f.label, rows: "3" })
          : el("input", { id: "setup-f-" + f.id, placeholder: f.label })));
    }
    select.addEventListener("change", drawFields);
    submit.addEventListener("click", submitChange);

    host.replaceChildren(
      el("p", { class: "muted", text: "Capability changes route through the gateway and require human approval (see the Approvals tab)." }),
      el("div", { class: "form-row" }, el("label", { class: "chk", text: "kind" }), select),
      fieldsBox,
      el("div", { class: "form-row" }, submit));
    drawFields();
  }

  function readFields(spec) {
    const v = {};
    for (const f of spec.fields) v[f.id] = ($("setup-f-" + f.id) || {}).value || "";
    return v;
  }

  async function submitChange() {
    const ag = $("setup-ag").value.trim();
    if (!ag) {
      setStatus("enter an agent group id first", "error");
      return;
    }
    const kind = $("setup-kind").value;
    const spec = KINDS[kind];
    const v = readFields(spec);
    if (!spec.ok(v)) {
      setStatus("fill the required field(s) for " + kind, "error");
      return;
    }
    try {
      const res = await api("/v1/ui/config/change", {
        method: "POST",
        body: JSON.stringify({ kind, agentGroupID: ag, requestedBy: "console", after: spec.build(v) }),
      });
      setStatus("submitted " + kind + " change " + (res && res.id ? res.id : "") + " — pending approval", "ok");
      await loadConfig(ag);
    } catch (e) {
      setStatus(String(e.message || e), "error");
    }
  }

  function show() {
    loadOnboard();
    if (!$("setup-change-form").dataset.rendered) {
      renderForm();
      $("setup-change-form").dataset.rendered = "1";
    }
  }

  function init() {
    $("setup-onboard-refresh").addEventListener("click", loadOnboard);
    $("setup-ag-load").addEventListener("click", () => loadConfig($("setup-ag").value.trim()));
  }

  document.addEventListener("DOMContentLoaded", init);
  return { show };
})();
