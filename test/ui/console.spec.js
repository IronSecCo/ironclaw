// Browser tests for the IronClaw web console (real UI interactions).
//
// Drives the actual SPA in Chrome: loads it, navigates every panel, creates an
// agent through the form, exercises the approvals approve/reject flow (and the
// sidebar badge), and sends a real chat message that a launched sandbox answers
// via the model proxy → OneCLI → Codex. Asserts on the DOM and watches for any
// console error throughout.
const { test, expect } = require("@playwright/test");

// trackErrors collects real problems for assertions: uncaught JS errors, JS
// console errors, and HTTP error responses — but ignores the browser's benign
// /favicon.ico probe (a real favicon is shipped; older builds may still 404 it).
function trackErrors(page) {
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + String(e)));
  page.on("console", (m) => {
    // Generic resource-load failures carry no URL here; they're caught precisely
    // by the 'response' handler below (which can see the URL).
    if (m.type() === "error" && !/Failed to load resource/i.test(m.text())) {
      errors.push("console: " + m.text());
    }
  });
  page.on("response", (r) => {
    // Ignore expected non-200s in this config: the favicon probe and the skills
    // endpoint (503 "skills not enabled" when the daemon runs without --skills-dir,
    // which the Skills panel renders as a friendly message).
    const benign = /favicon/i.test(r.url()) || /\/v1\/skills\b/.test(r.url());
    if (r.status() >= 400 && !benign) {
      errors.push("http " + r.status() + " " + r.url());
    }
  });
  return errors;
}

const PANELS = [
  "dashboard", "agents", "chat", "approvals", "sessions",
  "channels", "skills", "setup", "audit", "about",
];

test("dashboard loads, connects, shows stats, no console errors", async ({ page }) => {
  const errors = trackErrors(page);
  await page.goto("/ui/");
  await expect(page.locator("#status")).toContainText("connected", { timeout: 15_000 });
  for (const id of ["#stat-agents", "#stat-approvals", "#stat-sessions", "#stat-channels"]) {
    await expect(page.locator(id)).not.toBeEmpty();
  }
  expect(errors, "console errors:\n" + errors.join("\n")).toHaveLength(0);
});

test("every nav panel renders without console errors", async ({ page }) => {
  const errors = trackErrors(page);
  await page.goto("/ui/");
  for (const p of PANELS) {
    await page.locator(`.nav-item[data-panel="${p}"]`).click();
    await expect(page.locator(`#${p}`)).toBeVisible();
  }
  expect(errors, "console errors:\n" + errors.join("\n")).toHaveLength(0);
});

test("create-agent: form submits and the new card appears", async ({ page }) => {
  const errors = trackErrors(page);
  await page.goto("/ui/");
  await page.locator('.nav-item[data-panel="agents"]').click();
  await page.locator("#agents-new").click(); // opens the builder modal
  await expect(page.locator("#agent-modal")).toBeVisible();
  const name = "Playwright Bot " + Date.now();
  await page.locator("#ab-name").fill(name);
  await expect(page.locator("#ab-id-preview")).toContainText("Will be created as");
  await page.locator("#ab-create").click();
  await expect(page.locator("#agents-grid")).toContainText(name, { timeout: 15_000 });
  expect(errors, "console errors:\n" + errors.join("\n")).toHaveLength(0);
});

test("approvals: approve + reject in the UI, and the sidebar badge clears", async ({ page, request }) => {
  // Start from a clean inbox. The dev control-plane persists changes across runs
  // (and other tests/sessions submit them), so leftover pending changes would
  // break the positional approve-first/reject-first logic and the "no pending
  // changes" / empty-badge assertions below. Reject any residue via the API first.
  const residue = await (await request.get("/v1/changes/pending")).json();
  for (const c of residue) {
    await request.post(`/v1/changes/${c.ID}/decision`, {
      data: { outcome: "reject", decidedBy: "cleanup" },
    });
  }

  // Submit two changes through the gateway; both are held for a human.
  for (const Kind of ["persona", "packages"]) {
    const r = await request.post("/v1/changes", {
      data: { Kind, AgentGroupID: "dev-agent", RequestedBy: "playwright" },
    });
    expect(r.ok(), `submit ${Kind}`).toBeTruthy();
  }

  await page.goto("/ui/");
  await page.locator('.nav-item[data-panel="approvals"]').click();
  await expect(page.locator("#approvals-list")).toContainText("persona");
  await expect(page.locator("#nav-approvals-count")).toHaveText(/[1-9]/);

  // Approve the first card, then reject the remaining one.
  await page.getByRole("button", { name: "Approve" }).first().click();
  await page.getByRole("button", { name: "Reject" }).first().click();

  await expect(page.locator("#approvals-list")).toContainText("No pending changes", { timeout: 15_000 });
  await expect(page.locator("#nav-approvals-count")).toHaveText(""); // badge synced (the fix)
});

// Deterministic end-to-end chat: drives the FULL pipeline (UI → gateway →
// encrypted inbound queue → launched sandbox container → provider → encrypted
// outbound queue → UI) against the offline "mock" provider, so it needs no
// model credential and never flakes on an external token. The mock echoes the
// prompt, so the unique marker round-trips back into the transcript.
test("chat (mock-agent): a sent message round-trips through the real sandbox", async ({ page }) => {
  await page.goto("/ui/");
  await page.locator('.nav-item[data-panel="chat"]').click();
  await page.locator("#chat-ag").selectOption("mock-agent");
  const marker = "PWCHAT" + Date.now();
  await page.locator("#chat-input").fill(`echo ${marker}`);
  await page.locator("#chat-send").click();
  // First the user's own message must render, then the agent's reply carrying
  // the marker (the sandbox cold-starts a container, so allow generous time).
  await expect(page.locator("#chat-transcript")).toContainText(marker);
  await expect(
    page.locator("#chat-transcript .chat-msg.chat-agent .chat-body")
  ).toContainText(marker, { timeout: 90_000 });
});

// Live model path (real agent via OneCLI → Codex). Opt-in: it depends on a valid
// upstream credential that can expire/be revoked, so it would otherwise make the
// suite flaky. Run it with IRONCLAW_LIVE_MODEL=1 once a working token is present.
test("chat (dev-agent, live model): a sent message gets a real agent reply", async ({ page }) => {
  test.skip(
    !process.env.IRONCLAW_LIVE_MODEL,
    "live-model test: set IRONCLAW_LIVE_MODEL=1 with a working provider token"
  );
  await page.goto("/ui/");
  await page.locator('.nav-item[data-panel="chat"]').click();
  await page.locator("#chat-ag").selectOption("dev-agent");
  const marker = "PWCHAT" + Date.now();
  await page.locator("#chat-input").fill(`Reply with EXACTLY this token and nothing else: ${marker}`);
  await page.locator("#chat-send").click();
  await expect(
    page.locator("#chat-transcript .chat-msg.chat-agent .chat-body")
  ).toContainText(marker, { timeout: 75_000 });
});

test("audit: entries render and export controls are present", async ({ page }) => {
  await page.goto("/ui/");
  await page.locator('.nav-item[data-panel="audit"]').click();
  await expect(page.locator("#audit-list")).toContainText("chg_", { timeout: 15_000 });
  await expect(page.getByRole("button", { name: /Export JSON/i })).toBeVisible();
  await expect(page.getByRole("button", { name: /Export CSV/i })).toBeVisible();
});
