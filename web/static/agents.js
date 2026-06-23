// IronClaw web console — Agents (builder + picker).
//
// Lists agent groups (GET /v1/ui/agents) as cards with Chat / Edit actions, and
// creates or edits them in ONE step via PUT /v1/registry/agent-groups/{id} — name +
// persona + model + tools together, no separate gateway dance. The tool list and the
// starter templates come from the catalog endpoints (GET /v1/ui/{tools,templates}),
// so an operator never has to know internal tool names. Shares api()/el()/toast()/
// emptyState() with app.js.
"use strict";

const Agents = (() => {
  const $ = (id) => document.getElementById(id);
  let inited = false;
  let tools = [];          // catalog: [{name,title,description,category,egress,mandatory}]
  let templates = [];      // catalog: [{id,name,description,personaDocs,tools,model}]
  let personaSections = []; // catalog: [{key,title,filename,placeholder,help}]
  let catalogReady = false;
  // editing is null in create mode, else {id, folder, installedSkills} for the agent
  // being edited (so a PUT preserves fields the builder doesn't show).
  let editing = null;

  // ---- catalog ------------------------------------------------------------
  async function ensureCatalog() {
    if (catalogReady) return;
    const [tpls, tls, secs] = await Promise.all([
      api("/v1/ui/templates").catch(() => []),
      api("/v1/ui/tools").catch(() => []),
      api("/v1/ui/persona-sections").catch(() => []),
    ]);
    templates = tpls || [];
    tools = tls || [];
    personaSections = secs || [];
    catalogReady = true;
    renderTemplates();
    renderPersonaDocs();
    renderToolPicker();
  }

  // renderPersonaDocs builds one labeled textarea per persona document (identity /
  // soul / instructions), OpenClaw-style separation of concerns, instead of one blob.
  function renderPersonaDocs() {
    const box = $("ab-persona-docs");
    if (!box) return;
    box.replaceChildren();
    if (!personaSections.length) {
      // Fallback: a single textarea so persona is still editable if the schema fails.
      box.append(el("textarea", { id: "ab-persona", rows: "3", placeholder: "How it should behave (optional)" }));
      return;
    }
    for (const sec of personaSections) {
      box.append(el("div", { class: "persona-doc" },
        el("div", { class: "persona-doc-head" },
          el("span", { class: "pd-title", text: sec.title }),
          el("span", { class: "pd-file mono", text: sec.filename }),
          el("span", { class: "pd-help", text: sec.help })),
        el("textarea", { rows: "2", "data-doc": sec.key, placeholder: sec.placeholder })));
    }
  }

  function renderTemplates() {
    const box = $("ab-templates");
    if (!box) return;
    box.replaceChildren();
    if (!templates.length) { box.append(el("span", { class: "muted", text: "No templates available." })); return; }
    for (const t of templates) {
      const chip = el("button", { class: "chip", type: "button", title: t.description, "data-tpl": t.id },
        el("span", { class: "chip-name", text: t.name }),
        el("span", { class: "chip-sub", text: t.description }));
      chip.addEventListener("click", () => applyTemplate(t.id));
      box.append(chip);
    }
  }

  // renderToolPicker builds the grouped checklist. Mandatory tools render checked and
  // disabled (they can never be turned off); egress tools get a "needs approval" badge.
  function renderToolPicker() {
    const box = $("ab-tools");
    if (!box) return;
    box.replaceChildren();
    if (!tools.length) { box.append(el("span", { class: "muted", text: "No tools available." })); return; }
    const order = [];
    const byCat = new Map();
    for (const t of tools) {
      if (!byCat.has(t.category)) { byCat.set(t.category, []); order.push(t.category); }
      byCat.get(t.category).push(t);
    }
    for (const cat of order) {
      box.append(el("div", { class: "tool-cat", text: cat }));
      const grid = el("div", { class: "tool-grid" });
      for (const t of byCat.get(cat)) {
        const cb = el("input", { type: "checkbox", "data-tool": t.name });
        if (t.mandatory) { cb.checked = true; cb.disabled = true; }
        const badges = el("span", { class: "tool-badges" });
        if (t.mandatory) badges.append(el("span", { class: "tag tag-always", text: "always on" }));
        else if (t.egress) badges.append(el("span", { class: "tag tag-egress", text: "needs approval" }));
        const item = el("label", { class: "tool-item" },
          cb,
          el("span", { class: "tool-text" },
            el("span", { class: "t-title" }, el("span", { text: t.title }), badges),
            el("span", { class: "t-name mono", text: t.name }),
            el("span", { class: "t-desc", text: t.description })));
        grid.append(item);
      }
      box.append(grid);
    }
  }

  // ---- template / tool form state ----------------------------------------
  function applyTemplate(id) {
    const t = templates.find((x) => x.id === id);
    if (!t) return;
    for (const c of document.querySelectorAll("#ab-templates .chip")) {
      c.classList.toggle("active", c.getAttribute("data-tpl") === id);
    }
    setPersonaDocs(t.personaDocs || {});
    $("ab-model").value = t.model || "";
    const all = !t.tools || t.tools.length === 0;
    setAllTools(all);
    if (!all) setCheckedTools(t.tools);
  }

  // setPersonaDocs fills the per-section textareas from a {key: markdown} map.
  function setPersonaDocs(docs) {
    docs = docs || {};
    for (const ta of document.querySelectorAll("#ab-persona-docs textarea[data-doc]")) {
      ta.value = docs[ta.getAttribute("data-doc")] || "";
    }
    // Fallback single textarea (no schema): use whatever's under a common key.
    const legacy = $("ab-persona");
    if (legacy && !legacy.hasAttribute("data-doc")) legacy.value = docs.instructions || docs.soul || "";
  }

  // collectPersonaDocs reads the section textareas into a {key: markdown} map, omitting
  // empties. Empty map → the agent has no structured persona (server falls back).
  function collectPersonaDocs() {
    const out = {};
    for (const ta of document.querySelectorAll("#ab-persona-docs textarea[data-doc]")) {
      const v = ta.value.trim();
      if (v) out[ta.getAttribute("data-doc")] = v;
    }
    return out;
  }

  function setCheckedTools(names) {
    const want = new Set(names || []);
    for (const cb of document.querySelectorAll("#ab-tools input[data-tool]")) {
      if (cb.disabled) continue; // mandatory stays on
      cb.checked = want.has(cb.getAttribute("data-tool"));
    }
  }

  // setAllTools toggles the "no restriction" mode: when on, the per-tool boxes are
  // disabled (every tool is implied) so the operator isn't misled into thinking a
  // subset applies.
  function setAllTools(on) {
    const master = $("ab-alltools");
    if (master) master.checked = on;
    for (const cb of document.querySelectorAll("#ab-tools input[data-tool]")) {
      const t = tools.find((x) => x.name === cb.getAttribute("data-tool"));
      const mandatory = t && t.mandatory;
      cb.disabled = on || mandatory;
      if (on) cb.checked = true;
    }
    const picker = $("ab-tools");
    if (picker) picker.classList.toggle("dimmed", on);
  }

  // collectTools returns the enabled-tools array to send. Empty array means "all
  // tools" (no restriction) — matching the registry's EnabledTools semantics.
  function collectTools() {
    if ($("ab-alltools").checked) return [];
    const out = [];
    for (const cb of document.querySelectorAll("#ab-tools input[data-tool]")) {
      const t = tools.find((x) => x.name === cb.getAttribute("data-tool"));
      if (t && t.mandatory) continue; // always-on tools aren't part of the restriction list
      if (cb.checked) out.push(cb.getAttribute("data-tool"));
    }
    return out;
  }

  // ---- list ---------------------------------------------------------------
  function card(a) {
    const initial = (a.name || a.id || "?").trim().charAt(0).toUpperCase();
    const tags = el("div", { class: "tags" });
    if (a.model) tags.append(el("span", { class: "pill", text: a.model }));
    if (a.provider) tags.append(el("span", { class: "pill dim", text: a.provider }));
    const sn = a.sessions || 0, dn = a.destinations || 0;
    tags.append(el("span", { class: "pill " + (sn ? "ok" : "dim"), text: sn + (sn === 1 ? " session" : " sessions") }));
    tags.append(el("span", { class: "pill dim", text: dn + (dn === 1 ? " channel" : " channels") }));

    const chat = el("button", { class: "btn-primary btn-sm", type: "button", text: "Chat" });
    chat.addEventListener("click", () => openChatWith(a.id));
    const edit = el("button", { class: "ghost btn-sm", type: "button", text: "Edit" });
    edit.addEventListener("click", () => openForEdit(a.id));

    return el("div", { class: "agent-card" },
      el("div", { class: "top" },
        el("div", { class: "av", text: initial }),
        el("div", {},
          el("div", { class: "nm", text: a.name || a.id }),
          el("div", { class: "sub id mono", text: a.id }))),
      tags,
      el("div", { class: "foot" }, chat, edit));
  }

  // ---- modal open/close ---------------------------------------------------
  function openModal() { const m = $("agent-modal"); if (m && !m.open) m.showModal(); }
  function closeModal() { const m = $("agent-modal"); if (m && m.open) m.close(); }

  // openNew resets the form to create-mode and opens the builder modal.
  function openNew() {
    resetForm();
    ensureCatalog();
    openModal();
    $("ab-name").focus();
  }

  async function load() {
    ensureCatalog();
    const grid = $("agents-grid");
    if (!grid) return;
    grid.innerHTML = "";
    try {
      const list = await api("/v1/ui/agents");
      if (!list || !list.length) {
        const box = emptyState("No agents yet",
          "Create your first agent — pick a template, tweak its persona and tools, and it's ready to chat.",
          "＋ New agent", "agents", EMPTY_ICONS.agents);
        const cta = box.querySelector("button");
        if (cta) cta.addEventListener("click", openNew); // open the modal, not just switch panel
        grid.append(box);
        return;
      }
      for (const a of list) grid.append(card(a));
    } catch (e) {
      grid.append(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // ---- create / edit ------------------------------------------------------
  // slugify turns a friendly name into a stable id ("Research Bot" → "research-bot").
  function slugify(s) {
    return s.toLowerCase().trim()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48);
  }

  function preview() {
    if (editing) return; // id is fixed while editing
    const name = $("ab-name").value.trim();
    const id = slugify(name);
    const p = $("ab-id-preview");
    if (!p) return;
    if (!name) { p.innerHTML = "Pick a name to get started."; return; }
    if (!id) { p.innerHTML = "Please use some letters or numbers."; return; }
    p.innerHTML = 'Will be created as <code>' + id + '</code>.';
  }

  function resetForm() {
    editing = null;
    $("ab-summary").textContent = "Create an agent";
    $("ab-name").value = "";
    $("ab-name").disabled = false;
    setPersonaDocs({});
    $("ab-model").value = "";
    $("ab-provider").value = "";
    $("ab-create").textContent = "Create agent";
    for (const c of document.querySelectorAll("#ab-templates .chip")) c.classList.remove("active");
    if (catalogReady) setAllTools(false);
    setCheckedTools([]);
    preview();
  }

  async function openForEdit(agentId) {
    await ensureCatalog();
    let g;
    try {
      g = await api("/v1/registry/agent-groups/" + encodeURIComponent(agentId));
    } catch (e) { toast(String(e.message || e), "error"); return; }
    // registry.AgentGroup marshals with capitalized keys (no json tags).
    editing = { id: agentId, folder: g.Folder || agentId, installedSkills: g.InstalledSkills || [] };
    $("ab-summary").textContent = "Edit agent";
    $("ab-name").value = g.Name || agentId;
    $("ab-name").disabled = false;
    $("ab-id-preview").innerHTML = 'Editing <code>' + agentId + '</code>.';
    // Prefer structured docs; migrate a legacy single-blob persona into Instructions
    // so the operator sees it and the next save converts it to docs.
    if (g.PersonaDocs && Object.keys(g.PersonaDocs).length) setPersonaDocs(g.PersonaDocs);
    else setPersonaDocs(g.Persona ? { instructions: g.Persona } : {});
    $("ab-model").value = g.Model || "";
    $("ab-provider").value = g.Provider || "";
    $("ab-create").textContent = "Save changes";
    for (const c of document.querySelectorAll("#ab-templates .chip")) c.classList.remove("active");
    const enabled = g.EnabledTools || [];
    if (enabled.length === 0) { setAllTools(true); }
    else { setAllTools(false); setCheckedTools(enabled); }
    openModal();
    $("ab-name").focus();
  }

  async function save() {
    await ensureCatalog();
    const name = $("ab-name").value.trim();
    const id = editing ? editing.id : slugify(name);
    if (!name) { toast("Give your agent a name", "error"); $("ab-name").focus(); return; }
    if (!id) { toast("Use some letters or numbers in the name", "error"); $("ab-name").focus(); return; }

    const body = {
      Name: name,
      Folder: editing ? editing.folder : id,
      Provider: $("ab-provider").value.trim(),
      Model: $("ab-model").value.trim(),
      // Structured persona docs (identity/soul/instructions). Sending these and not
      // Persona means an edited agent's legacy blob is migrated into docs (or cleared).
      PersonaDocs: collectPersonaDocs(),
      EnabledTools: collectTools(),
    };
    if (editing && editing.installedSkills && editing.installedSkills.length) {
      body.InstalledSkills = editing.installedSkills; // preserve gateway-approved skills
    }
    const wasEditing = !!editing;
    try {
      await api("/v1/registry/agent-groups/" + encodeURIComponent(id), {
        method: "PUT", body: JSON.stringify(body),
      });
      toast(wasEditing ? ("Saved “" + name + "”") : ("Agent “" + name + "” created"), "ok");
      closeModal();
      resetForm();
      load();
    } catch (e) { toast(String(e.message || e), "error"); }
  }

  function init() {
    if (inited) return;
    inited = true;
    const nb = $("agents-new");
    if (nb) nb.addEventListener("click", openNew);
    const rb = $("agents-refresh");
    if (rb) rb.addEventListener("click", load);
    const cb = $("ab-create");
    if (cb) cb.addEventListener("click", save);
    for (const id of ["ab-cancel", "ab-close"]) {
      const b = $(id);
      if (b) b.addEventListener("click", () => { closeModal(); resetForm(); });
    }
    // Clicking the backdrop (outside the card) closes the modal.
    const modalEl = $("agent-modal");
    if (modalEl) modalEl.addEventListener("click", (e) => {
      if (e.target === modalEl) { closeModal(); resetForm(); }
    });
    const master = $("ab-alltools");
    if (master) master.addEventListener("change", () => setAllTools(master.checked));
    const nf = $("ab-name");
    if (nf) {
      nf.addEventListener("input", preview);
      nf.addEventListener("keydown", (e) => { if (e.key === "Enter") save(); });
    }
    ensureCatalog();
  }

  document.addEventListener("DOMContentLoaded", init);
  return { load, edit: openForEdit };
})();
