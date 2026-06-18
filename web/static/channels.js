// IronClaw web console — Channels & wiring management.
//
// Lists/creates wirings and destinations over the EXISTING /v1/registry/* admin
// endpoints, with enriched read-models at /v1/ui/channels|destinations. The
// "channel credentials" panel is read-only guidance: bot tokens live in the
// daemon's environment and are never stored here (sealed design, no vault).
// Shares api()/el()/setStatus() with app.js.
"use strict";

const Channels = (() => {
  const $ = (id) => document.getElementById(id);

  // --- messaging group + its wirings -------------------------------------

  async function loadChannel(id) {
    const host = $("ch-channel-view");
    if (!id) {
      host.replaceChildren(el("p", { class: "muted", text: "Pick a connected surface above to see its wirings." }));
      return;
    }
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const view = await api("/v1/ui/channels/" + encodeURIComponent(id));
      const mg = view.messagingGroup || {};
      const head = el("dl", { class: "meta" },
        el("dt", { text: "id" }), el("dd", { text: mg.ID || id }),
        el("dt", { text: "channel" }), el("dd", { text: (mg.ChannelType || "—") + " / " + (mg.PlatformID || "—") }),
        el("dt", { text: "unknown-sender" }), el("dd", { text: mg.UnknownSenderPolicy || "—" })
      );
      const wirings = view.wirings || [];
      const rows = wirings.length === 0
        ? [el("p", { class: "muted", text: "No wirings." })]
        : wirings.map((w) => el("div", { class: "card" },
            el("div", { class: "card-head" },
              el("span", { class: "kind", text: w.EngageMode || "?" }),
              el("span", { class: "id", text: w.ID || "" })),
            el("dl", { class: "meta" },
              el("dt", { text: "agent group" }),
              el("dd", { text: (w.agentGroupName ? w.agentGroupName + " (" + w.AgentGroupID + ")" : w.AgentGroupID) }),
              el("dt", { text: "session" }), el("dd", { text: w.SessionMode || "—" }),
              el("dt", { text: "priority" }), el("dd", { text: String(w.Priority ?? 0) }))));
      host.replaceChildren(head, ...rows);
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  async function createMessagingGroup() {
    const body = {
      channelType: $("ch-mg-channel").value.trim(),
      platformID: $("ch-mg-platform").value.trim(),
      instance: $("ch-mg-instance").value.trim(),
      isGroup: $("ch-mg-isgroup").checked,
      unknownSenderPolicy: $("ch-mg-policy").value,
    };
    if (!body.channelType || !body.platformID) {
      toast("Pick a platform and say where on it", "error");
      return;
    }
    try {
      const mg = await api("/v1/registry/messaging-groups", { method: "POST", body: JSON.stringify(body) });
      toast("Connected — now wire it to an agent", "ok");
      // Repopulate the surface pickers, pre-select the new one in step 2, and open
      // step 2 so the flow continues without copying an id around.
      if (typeof refreshMgGroups === "function") await refreshMgGroups();
      if (typeof selectMgIn === "function") { selectMgIn($("ch-mg-id"), mg.ID); selectMgIn($("ch-w-mg"), mg.ID); }
      const groups = document.querySelectorAll("#channels details");
      if (groups[1]) groups[1].open = true;
      await loadChannel(mg.ID);
    } catch (e) {
      toast(String(e.message || e), "error");
    }
  }

  // --- wiring -------------------------------------------------------------

  async function createWiring() {
    const body = {
      messagingGroupID: $("ch-w-mg").value.trim(),
      agentGroupID: $("ch-w-ag").value.trim(),
      engageMode: $("ch-w-engage").value,
      engagePattern: $("ch-w-pattern").value.trim(),
      sessionMode: $("ch-w-session").value,
      priority: Number($("ch-w-priority").value || 0),
    };
    if (!body.messagingGroupID) { toast("Pick a chat surface (step 1)", "error"); return; }
    if (!body.agentGroupID) { toast("Pick an agent", "error"); return; }
    try {
      const wr = await api("/v1/registry/wirings", { method: "POST", body: JSON.stringify(body) });
      toast("Wired up — the agent will engage on this surface", "ok");
      if (typeof refreshMgGroups === "function") refreshMgGroups();
      if ($("ch-mg-id").value.trim() === body.messagingGroupID) {
        await loadChannel(body.messagingGroupID);
      }
    } catch (e) {
      toast(String(e.message || e), "error");
    }
  }

  // --- destinations -------------------------------------------------------

  async function loadDestinations(agId) {
    const host = $("ch-dest-view");
    if (!agId) {
      host.replaceChildren(el("p", { class: "muted", text: "Pick an agent above to see where it can reply." }));
      return;
    }
    host.replaceChildren(el("p", { class: "muted", text: "Loading…" }));
    try {
      const dests = (await api("/v1/ui/destinations/" + encodeURIComponent(agId))) || [];
      if (dests.length === 0) {
        host.replaceChildren(el("p", { class: "muted", text: "No destinations." }));
        return;
      }
      host.replaceChildren(...dests.map((d) =>
        el("div", { class: "card" },
          el("dl", { class: "meta" },
            el("dt", { text: "channel" }), el("dd", { text: d.channelType }),
            el("dt", { text: "platform" }), el("dd", { text: d.platformID })))));
    } catch (e) {
      host.replaceChildren(el("p", { class: "error", text: String(e.message || e) }));
    }
  }

  async function addDestination() {
    const body = {
      agentGroupID: $("ch-d-ag").value.trim(),
      channelType: $("ch-d-channel").value.trim(),
      platformID: $("ch-d-platform").value.trim(),
    };
    if (!body.agentGroupID || !body.channelType || !body.platformID) {
      toast("Pick an agent, a platform, and where to send", "error");
      return;
    }
    try {
      await api("/v1/registry/destinations", { method: "POST", body: JSON.stringify(body) });
      toast("Allowed — the agent can reply there now", "ok");
      await loadDestinations(body.agentGroupID);
    } catch (e) {
      toast(String(e.message || e), "error");
    }
  }

  // --- guided credentials (read-only) ------------------------------------

  const ADAPTERS = [
    { name: "Slack", env: "SLACK_BOT_TOKEN" },
    { name: "Discord", env: "DISCORD_BOT_TOKEN" },
    { name: "Telegram", env: "TELEGRAM_BOT_TOKEN" },
    { name: "Microsoft Teams", env: "IRONCLAW_TEAMS_WEBHOOK_URL" },
    { name: "Signal", env: "IRONCLAW_SIGNAL_CLI_URL" },
    { name: "iMessage (macOS)", env: "IRONCLAW_IMESSAGE_ENABLE" },
  ];

  function renderCredGuide() {
    const host = $("ch-cred-guide");
    if (!host || host.dataset.rendered) return;
    host.dataset.rendered = "1";
    host.replaceChildren(...ADAPTERS.map((a) => {
      const input = el("input", { type: "password", placeholder: a.env + " (masked; not sent)", autocomplete: "off" });
      const cmd = el("pre", { class: "payload", text: "export " + a.env + "=<token>" });
      const reveal = el("button", { class: "ghost", type: "button", text: "Show export line" });
      reveal.addEventListener("click", () => {
        const v = input.value.trim();
        cmd.textContent = "export " + a.env + "=" + (v ? v : "<token>");
      });
      return el("div", { class: "card" },
        el("div", { class: "card-head" }, el("span", { class: "kind", text: a.name })),
        el("div", { class: "form-row" }, input, reveal),
        cmd);
    }));
  }

  function show() {
    renderCredGuide();
  }

  function init() {
    $("ch-mg-load").addEventListener("click", () => loadChannel($("ch-mg-id").value.trim()));
    $("ch-mg-create").addEventListener("click", createMessagingGroup);
    $("ch-w-create").addEventListener("click", createWiring);
    $("ch-d-load").addEventListener("click", () => loadDestinations($("ch-d-ag").value.trim()));
    $("ch-d-add").addEventListener("click", addDestination);
  }

  document.addEventListener("DOMContentLoaded", init);
  return { show };
})();
