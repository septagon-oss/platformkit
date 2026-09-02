import { defineConfig, devices } from '@playwright/test';

// The application is started by scripts/e2e.sh, not by Playwright: the same
// script bootstraps a fresh database and tears it down again, and a webServer
// block here would be a second place that knows how to run the app.
//
// One browser and one worker. This is a gate, not a compatibility matrix: it
// proves the admin shell renders and a generated CRUD screen works.
export default defineConfig({
  testDir: '.',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [['list']],
  use: {
    baseURL: process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
