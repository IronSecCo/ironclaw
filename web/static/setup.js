// IronClaw web console — Setup wizard + config editor.
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

  // statusClass maps a wizard step status to a badge colour: ok/skipped are
  // satisfied (green), failed is a hard error (red), and planned/action are pending
  // operator work (amber) — pending must read differently from a real error.
  function statusClass(s) {
    switch (String(s)) {
      case "ok":
      case "skipped":
        return "ok";
      case "failed":
        return "bad";
      default:
        return "warn"; // planned / action
    }
  }

  function isReady(status) {
    return status === "ok" || status === "skipped";
  }

  // isPending marks a step that is still the operator's to-do (would act, or needs
  // a manual action) — these get a left accent border so the real to-do list
  // stands out from satisfied rows (Von Restorff).
  function isPending(status) {
    return status === "action" || status === "planned";
  }

  // statusLabel gives the badge its human text. A "skipped" step is a satisfied,
  // idempotent re-run (reused token, image already present) — it reads as "ready"
  // so it isn't mistaken for a bypassed/abandoned step. The word itself carries
  // the state, so the meaning survives without color (WCAG color-independence).
  function statusLabel(s) {
    return String(s) === "skipped" ? "ready" : String(s);
  }

  // DOCS is the public docs site the wizard links pending steps to. The console is
  // served from the binary (no local docs tree), so deep help points at the site.
  const DOCS = "https://ironsecco.github.io/ironclaw/";

  // STEP_AFFORDANCE hands a pending step its concrete fix: the exact env line(s) to
  // copy and the doc that explains them, so the to-do is actionable in-place
  // instead of only described. Only steps with a single copy-paste remedy appear —
  // runtime / image-build / API-reachability steps carry their command in the
  // detail text and need no chip.
  const STEP_AFFORDANCE = {
    "model-credential": {
      intro: "Set one provider credential in the control-plane's environment, then restart it:",
      snippets: [
        "export ANTHROPIC_API_KEY=sk-ant-…",
        "export OPENAI_API_KEY=sk-…",
        "export OPENROUTER_API_KEY=sk-or-…",
      ],
      docLabel: "Model setup",
      docHref: DOCS + "quickstart/",
    },
    channel: {
      intro: "Arm a channel by exporting its bot token host-side, then restart:",
      snippets: [
        "export SLACK_BOT_TOKEN=xoxb-…",
        "export TELEGRAM_BOT_TOKEN=…",
      ],
      docLabel: "Channel setup",
      docHref: DOCS + "channels/",
    },
  };

  // copyChip renders a monospace command with a one-click Copy button. The
  // clipboard API needs a secure context (https / localhost); on a plain-http
  // remote it falls back to execCommand so copy still works.
  function copyChip(snippet) {
    const btn = el("button", { class: "ghost btn-sm copy-btn", type: "button", text: "Copy" });
    const flash = (txt) => { btn.textContent = txt; setTimeout(() => { btn.textContent = "Copy"; }, 1600); };
    btn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(snippet);
        flash("Copied ✓");
      } catch (_) {
        const ta = el("textarea", { class: "visually-hidden", "aria-hidden": "true" });
        ta.value = snippet;
        document.body.append(ta);
        ta.select();
        let ok = false;
        try { ok = document.execCommand("copy"); } catch (_) { ok = false; }
        ta.remove();
        flash(ok ? "Copied ✓" : "Copy failed");
      }
    });
    return el("div", { class: "copy-row" }, el("code", { class: "copy-snip", text: snippet }), btn);
  }

  // stepAffordance builds the actionable block under a pending step's detail: the
  // copy chips and a doc link. Returns null for steps with no chip-able remedy.
  function stepAffordance(name) {
    const a = STEP_AFFORDANCE[name];
    if (!a) return null;
    const box = el("div", { class: "step-affordance" }, el("p", { class: "hint", text: a.intro }));
    for (const s of a.snippets) box.append(copyChip(s));
    box.append(el("a", {
      class: "doc-link", href: a.docHref, target: "_blank", rel: "noopener noreferrer",
      text: a.docLabel + " ↗",
    }));
    return box;
  }

  // STEP_LABELS gives each wizard step a human title; unknown steps fall back to
  // their raw name so a newly-added step still renders.
  const STEP_LABELS = {
    runtime: "Container runtime",
    "api-token": "API token",
    "model-credential": "Model credential",
    "sandbox-image": "Sandbox image",
    channel: "Channel",
    verify: "API reachable",
  };

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
      const ready = steps.filter((s) => isReady(s.status)).length;
      const allReady = ready === steps.length;
      const summary = el("div", { class: "onboard-summary" },
        el("span", { class: "kind " + (allReady ? "ok" : "warn"), text: ready + " / " + steps.length + " ready" }),
        el("span", { class: "muted", text: allReady ? "Host is ready to run agents." : "Some steps need an operator action." }));
      const cards = steps.map((s) => {
        const pending = isPending(s.status);
        const card = el("div", { class: "card onboard-step" + (pending ? " pending" : "") },
          el("div", { class: "card-head" },
            el("strong", { text: STEP_LABELS[s.name] || s.name }),
            el("span", { class: "kind " + statusClass(s.status), text: statusLabel(s.status) })),
          el("p", { class: "muted", text: s.detail || "" }));
        if (pending) {
          const aff = stepAffordance(s.name);
          if (aff) card.append(aff);
        }
        return card;
      });
      host.replaceChildren(summary, ...cards);
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // --- config editor ------------------------------------------------------

  async function loadConfig(id) {
    const host = $("setup-config-view");
    if (!id) {
      host.replaceChildren(el("p", { class: "muted", text: "Pick an agent above to view and edit it." }));
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
        el("dt", { text: "provider/model" }), el("dd", { text: (g.Provider || "default") + " / " + (g.Model || "default") }),
        el("dt", { text: "persona" }), el("dd", { text: g.Persona || "—" }),
        el("dt", { text: "enabled tools" }), el("dd", { text: (g.EnabledTools && g.EnabledTools.length) ? g.EnabledTools.join(", ") : "all (unrestricted)" }),
        el("dt", { text: "installed skills" }), el("dd", { text: (g.InstalledSkills && g.InstalledSkills.length) ? g.InstalledSkills.map((s) => s.Name + "@" + s.Version).join(", ") : "none" }));
      const changes = applied.length === 0
        ? el("p", { class: "muted", text: "No applied capability changes recorded." })
        : el("pre", { class: "payload", text: JSON.stringify(applied, null, 2) });
      host.replaceChildren(head, el("h3", { text: "Applied capability changes" }), changes);
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // KINDS maps each capability change to a friendly label, a one-line explainer,
  // and the fields it needs. Fields render as a labelled control: a textarea
  // (area), a dropdown (select), or a text input — with an optional hint, so an
  // operator never guesses what a field means or which values are valid.
  const KINDS = {
    persona: {
      label: "Set persona",
      help: "Give the agent its personality and standing instructions — who it is and how it should behave.",
      fields: [{ id: "instructions", label: "System instructions", area: true, placeholder: "You are a friendly support assistant for…" }],
      build: (v) => ({ instructions: v.instructions }),
      ok: (v) => v.instructions.trim() !== "",
    },
    enabled_tools: {
      label: "Limit tools",
      help: "Restrict the agent to a specific set of tools.",
      fields: [{ id: "tools", label: "Allowed tools", placeholder: "send_message, schedule_task", hint: "Comma-separated tool names." }],
      build: (v) => splitList(v.tools),
      ok: (v) => splitList(v.tools).length > 0,
    },
    packages: {
      label: "Grant packages",
      help: "Host-curated packages the agent may use.",
      fields: [{ id: "packages", label: "Packages", placeholder: "package-a, package-b", hint: "Comma-separated." }],
      build: (v) => splitList(v.packages),
      ok: (v) => splitList(v.packages).length > 0,
    },
    permissions: {
      label: "Grant access",
      help: "Give a person a role on this agent.",
      fields: [
        { id: "grant", label: "Role", options: [["owner", "Owner"], ["admin", "Admin"]] },
        { id: "member", label: "Person", placeholder: "e.g. slack:alice" },
      ],
      build: (v) => ({ grant: v.grant.trim(), member: v.member.trim() }),
      ok: (v) => v.grant.trim() !== "" && v.member.trim() !== "",
    },
    mounts: {
      label: "Add mounts",
      help: "Filesystem paths to mount into the sandbox.",
      fields: [{ id: "sources", label: "Source paths", placeholder: "/data/docs, /data/refs", hint: "Comma-separated absolute paths." }],
      build: (v) => splitList(v.sources).map((s) => ({ source: s })),
      ok: (v) => splitList(v.sources).length > 0,
    },
    wiring: {
      label: "Change engagement",
      help: "How and when the agent jumps into a conversation.",
      fields: [
        { id: "engage", label: "When to engage", options: [["mention", "On @mention"], ["mention-sticky", "On @mention, then stay in the thread"], ["pattern", "When a message matches a pattern"]] },
        { id: "pattern", label: "Pattern (for pattern mode)", placeholder: "deploy|incident" },
      ],
      build: (v) => ({ engage: v.engage.trim(), pattern: v.pattern.trim() }),
      ok: (v) => v.engage.trim() !== "",
    },
    mcp_access: {
      label: "Grant MCP server",
      help: "Let the agent use a configured MCP server's tools (richer flow with tool discovery is on the MCP tab).",
      fields: [
        { id: "server", label: "MCP server name", placeholder: "github", hint: "Must be a server configured on the MCP tab." },
        { id: "tools", label: "Tools", placeholder: "create_issue, list_issues", hint: "Comma-separated; leave blank for all of the server's tools." },
      ],
      build: (v) => ({ server: v.server.trim(), tools: splitList(v.tools) }),
      ok: (v) => v.server.trim() !== "",
    },
  };

  function splitList(s) {
    return String(s || "").split(",").map((x) => x.trim()).filter(Boolean);
  }

  function fieldControl(f) {
    const fid = "setup-f-" + f.id;
    let control;
    if (f.area) control = el("textarea", { id: fid, placeholder: f.placeholder || "", rows: "4" });
    else if (f.options) {
      control = el("select", { id: fid });
      for (const [v, t] of f.options) control.append(el("option", { value: v, text: t }));
    } else control = el("input", { id: fid, placeholder: f.placeholder || "" });
    const node = el("div", { class: "field" }, el("label", { text: f.label }), control);
    if (f.hint) node.append(el("p", { class: "hint", text: f.hint }));
    return node;
  }

  function renderForm() {
    const host = $("setup-change-form");
    const select = el("select", { id: "setup-kind" });
    for (const [k, spec] of Object.entries(KINDS)) select.append(el("option", { value: k, text: spec.label }));
    const help = el("p", { class: "hint", id: "setup-kind-help" });
    const fieldsBox = el("div", { id: "setup-kind-fields", class: "form-row" });
    const submit = el("button", { class: "btn-primary", type: "button", text: "Request change" });

    function drawFields() {
      const spec = KINDS[select.value];
      help.textContent = spec.help;
      fieldsBox.replaceChildren(...spec.fields.map(fieldControl), el("div", { class: "field" }, el("label", { html: "&nbsp;" }), submit));
    }
    select.addEventListener("change", drawFields);
    submit.addEventListener("click", submitChange);

    host.replaceChildren(
      el("p", { class: "muted", text: "Every change is held for human approval — it shows up in the Approvals tab, never applied from here." }),
      el("div", { class: "form-row" }, el("div", { class: "field" }, el("label", { text: "What to change" }), select)),
      help,
      fieldsBox);
    drawFields();
  }

  function readFields(spec) {
    const v = {};
    for (const f of spec.fields) v[f.id] = ($("setup-f-" + f.id) || {}).value || "";
    return v;
  }

  async function submitChange() {
    const ag = $("setup-ag").value.trim();
    if (!ag) { toast("Pick an agent first", "error"); return; }
    const kind = $("setup-kind").value;
    const spec = KINDS[kind];
    const v = readFields(spec);
    if (!spec.ok(v)) { toast("Fill in the field(s) above", "error"); return; }
    try {
      await api("/v1/ui/config/change", {
        method: "POST",
        body: JSON.stringify({ kind, agentGroupID: ag, requestedBy: "console", after: spec.build(v) }),
      });
      toast("Requested — find it in Approvals to approve", "ok");
      await loadConfig(ag);
    } catch (e) {
      toast(String(e.message || e), "error");
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
