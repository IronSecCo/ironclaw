// Browser tests for the capability-change flow a human actually drives:
// add/limit tools and install a skill through the Setup → Config editor, watch
// them land in the Approvals inbox, APPROVE them as the human, and confirm the
// gateway materialized the change on the agent group.
//
// Each test provisions its OWN agent group (via the registry admin API) so it is
// isolated from any other pending changes on the shared control-plane, and locates
// its specific approval card by that unique group id.
const { test, expect } = require("@playwright/test");

// makeGroup creates a fresh, uniquely-named mock-backed agent group and returns
// its id. mock provider = the group can also launch a real sandbox with no creds.
async function makeGroup(request, tag) {
  const id = `cap-${tag}-${Date.now()}`;
  const r = await request.put(`/v1/registry/agent-groups/${id}`, {
    data: { Name: `Cap ${id}`, Folder: id, Provider: "mock" },
  });
  expect(r.ok(), `create group ${id}`).toBeTruthy();
  return id;
}

// approveCardFor finds the pending approval card for a given agent-group id and
// clicks Approve, then waits for it to drop off the list.
async function approveCardFor(page, gid, appearTimeout = 15_000) {
  const reload = async () => {
    await page.locator('.nav-item[data-panel="dashboard"]').click();
    await page.locator('.nav-item[data-panel="approvals"]').click();
  };
  await page.locator('.nav-item[data-panel="approvals"]').click();
  const card = page.locator("article.card", { hasText: gid });
  // Poll the inbox until the card shows (agent-initiated changes arrive only after
  // the sandbox cold-starts and forwards the envelope).
  await expect(async () => {
    if (!(await card.count())) await reload();
    await expect(card).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: appearTimeout });
  await card.getByRole("button", { name: "Approve" }).click();
  await expect(page.locator("article.card", { hasText: gid })).toHaveCount(0, { timeout: 15_000 });
}

test("capabilities: add tools via Setup, approve as human, gateway applies", async ({ page, request }) => {
  const gid = await makeGroup(request, "tools");

  await page.goto("/ui/");
  await expect(page.locator("#status")).toContainText("connected", { timeout: 15_000 });

  // Setup → Config editor → pick the agent → "Limit tools" → request the change.
  await page.locator('.nav-item[data-panel="setup"]').click();
  // The Config editor lives in a collapsed <details>; open it and pick the agent.
  await page.locator('#setup-ag').evaluate((s) => (s.closest("details").open = true));
  await page.locator("#setup-ag").selectOption(gid);
  await page.locator("#setup-kind").selectOption("enabled_tools");
  await page.locator("#setup-f-tools").fill("web_search, read_file, send_message");
  await page.getByRole("button", { name: "Request change" }).click();

  // Approve it as the human, then confirm the gateway materialized it.
  await approveCardFor(page, gid);

  const after = await (await request.get(`/v1/registry/agent-groups/${gid}`)).json();
  expect(after.EnabledTools).toEqual(["web_search", "read_file", "send_message"]);
});

test("capabilities: a sandbox launches with the approved tool set", async ({ page, request }) => {
  // End-to-end past approval: after the tools are applied, a real launched sandbox
  // must actually be restricted to them. We assert via the mock-agent chat round-trip
  // that the sandbox came up at all (its --enabled-tools is checked at the host level
  // in Go tests; here we prove the approved group still serves chat).
  const gid = await makeGroup(request, "sbx");
  // Restrict to a set that still includes the mandatory + a couple of real tools.
  const r = await request.post("/v1/changes", {
    data: { Kind: "enabled_tools", AgentGroupID: gid, RequestedBy: "playwright", After: { tools: ["read_file"] } },
  });
  expect(r.ok()).toBeTruthy();

  await page.goto("/ui/");
  await approveCardFor(page, gid);

  // Chat to the restricted group; the mock sandbox should still answer.
  await page.locator('.nav-item[data-panel="chat"]').click();
  await page.locator("#chat-ag").selectOption(gid);
  const marker = "CAPSBX" + Date.now();
  await page.locator("#chat-input").fill(`echo ${marker}`);
  await page.locator("#chat-send").click();
  await expect(
    page.locator("#chat-transcript .chat-msg.chat-agent .chat-body")
  ).toContainText(marker, { timeout: 90_000 });
});

test("capabilities: the AGENT asks for a tool, the human approves it", async ({ page, request }) => {
  // The full sanctioned escape hatch: the agent itself calls
  // request_capability_change (a mandatory tool), the request surfaces in the
  // Approvals inbox, and the human grants it — then it is applied. Driven with the
  // offline mock so it needs no model credential.
  const gid = await makeGroup(request, "asks");

  await page.goto("/ui/");
  await expect(page.locator("#status")).toContainText("connected", { timeout: 15_000 });

  // Agent asks for web_search via the directive the mock understands.
  await page.locator('.nav-item[data-panel="chat"]').click();
  await page.locator("#chat-ag").selectOption(gid);
  await page.locator("#chat-input").fill(
    'tool:request_capability_change {"kind":"enabled_tools","payload":{"tools":["web_search","read_file"]},"reason":"agent wants search"}'
  );
  await page.locator("#chat-send").click();

  // The request shows up in Approvals (after the sandbox cold-starts); approve it.
  await approveCardFor(page, gid, 90_000);

  const after = await (await request.get(`/v1/registry/agent-groups/${gid}`)).json();
  expect(after.EnabledTools).toEqual(["web_search", "read_file"]);
});
