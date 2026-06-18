// Playwright config for the IronClaw web-console browser tests.
//
// Uses the system Google Chrome (channel: "chrome") so no Playwright browser
// download is needed — just `npm i`. Point it at a running control-plane that
// serves /ui/ via IRONCLAW_UI_URL (default http://127.0.0.1:8788).
const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: ".",
  timeout: 90_000,
  expect: { timeout: 20_000 },
  retries: 0,
  workers: 1, // the console is a single shared control-plane; keep tests serial
  reporter: [["list"]],
  use: {
    baseURL: process.env.IRONCLAW_UI_URL || "http://127.0.0.1:8788",
    channel: "chrome",
    headless: true,
    actionTimeout: 20_000,
  },
});
