// IronClaw web console — Skills management.
//
// Surfaces the host-side skills endpoints (/v1/skills): the curated catalog, install
// (which submits a gateway change for HUMAN APPROVAL — never applied from here), and
// remove-from-catalog. Shares api()/el()/toast() with app.js. When the daemon
// has no skills source configured the endpoints return 503 and this shows a hint.
"use strict";

const Skills = (() => {
  function renderList(skills) {
    const list = document.getElementById("skills-list");
    list.replaceChildren();
    if (!skills || skills.length === 0) {
      list.append(emptyState("Catalog is empty",
        "No skills in the curated catalog yet. Add a signed skill above — installs go through human approval before they apply.",
        null, null, EMPTY_ICONS.skills));
      return;
    }
    for (const s of skills) {
      list.append(
        el("article", { class: "card" },
          el("div", { class: "card-head" },
            el("span", { class: "id", text: (s.name || "") + "@" + (s.version || "") })),
          el("div", { class: "form-row" },
            el("button", { class: "ghost", type: "button", "data-remove": s.name || "", "data-version": s.version || "" }, "Remove from catalog")))
      );
    }
  }

  async function load() {
    const list = document.getElementById("skills-list");
    try {
      const skills = await api("/v1/skills");
      renderList(skills || []);
    } catch (e) {
      const msg = String(e.message || e);
      list.replaceChildren(msg.indexOf("503") === 0
        ? emptyState("Skills not enabled",
            "Skills load when the host is started with --skills-dir and --skills-trust-key. See the Skills docs to enable them.",
            null, null, EMPTY_ICONS.offline)
        : el("p", { class: "error", text: "Could not load skills: " + msg }));
    }
  }

  async function install() {
    const ref = document.getElementById("skill-ref").value.trim();
    const group = document.getElementById("skill-group").value.trim();
    const by = document.getElementById("skill-by").value.trim();
    if (!ref || !group) { toast("skill name@version and agent group are required", "error"); return; }
    const at = ref.lastIndexOf("@");
    if (at <= 0 || at === ref.length - 1) { toast("use the form name@version", "error"); return; }
    const body = JSON.stringify({ skill: ref.slice(0, at), version: ref.slice(at + 1), agentGroupId: group, requestedBy: by });
    try {
      const res = await api("/v1/skills/install", { method: "POST", body });
      toast("install proposed" + (res && res.id ? " (change " + res.id + ")" : "") + " — awaits approval", "ok");
    } catch (e) {
      toast(String(e.message || e), "error");
    }
  }

  async function remove(name, version) {
    if (!name) return;
    try {
      const q = version ? "?version=" + encodeURIComponent(version) : "";
      await api("/v1/skills/" + encodeURIComponent(name) + q, { method: "DELETE" });
      toast("removed " + name + " from the catalog", "ok");
      load();
    } catch (e) { toast(String(e.message || e), "error"); }
  }

  let wired = false;
  function show() {
    if (!wired) {
      document.getElementById("skill-install").addEventListener("click", install);
      document.getElementById("refresh-skills").addEventListener("click", load);
      document.getElementById("skills-list").addEventListener("click", (e) => {
        const b = e.target.closest("[data-remove]");
        if (b) remove(b.getAttribute("data-remove"), b.getAttribute("data-version"));
      });
      wired = true;
    }
    load();
  }

  return { show, load };
})();
