// IronClaw web console — Logs & audit viewer.
//
// A read-only view over the append-only gateway audit log (the control-plane's
// structured, persisted event log) via the read-model /v1/ui/audit. Adds
// client-side filter + full-text search + export (JSON/CSV) and a live-tail
// toggle that polls while the tab is open. Shares api()/el()/setStatus() with
// app.js. Read-only: this module never mutates anything.
"use strict";

const Audit = (() => {
  const STAGES = ["submit", "verdict", "decision", "apply"];
  const POLL_MS = 4000;

  let entries = []; // last fetched, newest first
  let tailTimer = null;

  function fmtTime(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  // ensureControls builds the filter/search/export/tail bar once, above the list.
  function ensureControls() {
    if (document.getElementById("audit-controls")) return;
    const list = document.getElementById("audit-list");
    if (!list) return;

    const search = el("input", {
      id: "audit-search", type: "search", autocomplete: "off", spellcheck: "false",
      placeholder: "Filter — search id, kind, detail, stage…",
    });
    search.style.flex = "1";
    search.style.minWidth = "200px";
    search.style.padding = "7px 10px";
    search.style.background = "var(--bg)";
    search.style.border = "1px solid var(--border)";
    search.style.borderRadius = "var(--radius)";
    search.style.color = "var(--text)";
    search.addEventListener("input", render);

    const stage = el("select", { id: "audit-stage" });
    stage.style.padding = "7px 10px";
    stage.style.background = "var(--bg)";
    stage.style.border = "1px solid var(--border)";
    stage.style.borderRadius = "var(--radius)";
    stage.style.color = "var(--text)";
    stage.append(el("option", { value: "", text: "All stages" }));
    for (const s of STAGES) stage.append(el("option", { value: s, text: s }));
    stage.addEventListener("change", render);

    const tailLabel = el("label", { class: "muted" });
    tailLabel.style.display = "flex";
    tailLabel.style.alignItems = "center";
    tailLabel.style.gap = "6px";
    const tail = el("input", { id: "audit-tail", type: "checkbox" });
    tail.addEventListener("change", () => setTail(tail.checked));
    tailLabel.append(tail, document.createTextNode("Live tail"));

    const exportJSON = el("button", { class: "ghost", type: "button", text: "Export JSON" });
    exportJSON.addEventListener("click", () => exportData("json"));
    const exportCSV = el("button", { class: "ghost", type: "button", text: "Export CSV" });
    exportCSV.addEventListener("click", () => exportData("csv"));

    const count = el("span", { id: "audit-count", class: "muted" });
    count.style.marginLeft = "auto";
    count.style.fontSize = "12px";

    const controls = el("div", { id: "audit-controls" });
    controls.style.display = "flex";
    controls.style.flexWrap = "wrap";
    controls.style.alignItems = "center";
    controls.style.gap = "8px";
    controls.style.margin = "12px 0 4px";
    controls.append(search, stage, tailLabel, exportJSON, exportCSV, count);

    list.parentNode.insertBefore(controls, list);
  }

  function currentFilters() {
    const s = document.getElementById("audit-search");
    const st = document.getElementById("audit-stage");
    return { text: (s ? s.value : "").trim().toLowerCase(), stage: st ? st.value : "" };
  }

  function matches(e, f) {
    if (f.stage && e.stage !== f.stage) return false;
    if (!f.text) return true;
    const hay = [e.time, e.stage, e.changeId, e.kind, e.detail].join(" ").toLowerCase();
    return hay.includes(f.text);
  }

  function filtered() {
    const f = currentFilters();
    return entries.filter((e) => matches(e, f));
  }

  function renderRow(e) {
    const card = el("article", { class: "card" });
    card.append(
      el("div", { class: "card-head" },
        el("span", { class: "kind", text: e.stage || "—" }),
        el("span", { class: "id", text: e.changeId || "" })
      ),
      el("dl", { class: "meta" },
        el("dt", { text: "Time" }), el("dd", { text: fmtTime(e.time) }),
        el("dt", { text: "Kind" }), el("dd", { text: e.kind || "—" }),
        el("dt", { text: "Detail" }), el("dd", { text: e.detail || "—" })
      )
    );
    return card;
  }

  function render() {
    const host = document.getElementById("audit-list");
    if (!host) return;
    const rows = filtered();
    const count = document.getElementById("audit-count");
    if (count) count.textContent = "showing " + rows.length + " of " + entries.length;
    if (rows.length === 0) {
      host.replaceChildren(el("p", { class: "muted", text: entries.length ? "No entries match the filter." : "No audit entries." }));
      return;
    }
    host.replaceChildren(...rows.map(renderRow));
  }

  async function load() {
    ensureControls();
    const host = document.getElementById("audit-list");
    if (!host) return;
    if (entries.length === 0) host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      entries = (await api("/v1/ui/audit?limit=500")) || [];
      render();
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  // setTail starts/stops polling. The tick skips the fetch while the Audit tab is
  // hidden so a forgotten toggle doesn't poll the host in the background.
  function setTail(on) {
    if (tailTimer) {
      clearInterval(tailTimer);
      tailTimer = null;
    }
    if (!on) return;
    tailTimer = setInterval(() => {
      const panel = document.getElementById("audit");
      if (panel && !panel.hidden) load();
    }, POLL_MS);
  }

  function download(name, mime, data) {
    const url = URL.createObjectURL(new Blob([data], { type: mime }));
    const a = el("a", { href: url, download: name });
    document.body.append(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  function csvCell(v) {
    const s = String(v == null ? "" : v);
    return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  }

  function exportData(format) {
    const rows = filtered();
    if (format === "csv") {
      const head = ["time", "stage", "changeId", "kind", "detail"];
      const lines = [head.join(",")].concat(
        rows.map((e) => head.map((k) => csvCell(e[k])).join(","))
      );
      download("ironclaw-audit.csv", "text/csv", lines.join("\n"));
    } else {
      download("ironclaw-audit.json", "application/json", JSON.stringify(rows, null, 2));
    }
  }

  return { load };
})();
