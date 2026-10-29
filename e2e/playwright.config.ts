import { defineConfig, devices } from '@playwright/test';

// The application is started by scripts/e2e.sh, not by Playwright: the same
// script bootstraps a fresh database and tears it down again, and a webServer
// block here would be a second place that knows how to run the app.
//
// One browser and one worker. This is a gate, not a compatibility matrix: it
// proves the admin shell renders and a generated CRUD screen works.
export default defineConfig({
  testDir: '.',
  testIgnore: ['picture/**', 'formsteps/**'],
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
    // A renderer's shared buffers live in /dev/shm, which a container gets at
    // Docker's own 64 MB default unless the caller says otherwise: measured as
    // `job container /dev/shm 0 MB used of 64 MB` in the gate's own log, on a job
    // with no readable memory ceiling. A page whose frame buffers outgrow that
    // pool takes the renderer with it, and the case then reports only that its
    // page closed — no assertion, no verdict. The buffers go to disk instead;
    // nothing measured, asserted or waited-on here changes.
    launchOptions: { args: ['--disable-dev-shm-usage'] },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
