import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

// The loop's own design gate, run on the framed pages it cannot reach by URL: each page is fetched
// behind the administrator's cookie, written to disk with a <base> that points its sheet back at
// the live server, measured by the gate's own probe, and judged by the gate's own rules
// (`design_gate.py --rules`). Nothing here re-implements a rule; the counts printed are the gate's.
//
// Opt-in: it needs the gate's files on this host, so it runs only when DESIGN_GATE_DIR names them.

const gate = process.env.DESIGN_GATE_DIR ?? '';
const out = process.env.DESIGN_GATE_OUT ?? '';
const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

async function signIn(page: Page) {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
}

function judge(name: string, html: string, origin: string): { refusals: string[]; text: string } {
  mkdirSync(out, { recursive: true });
  const file = join(out, `${name}.html`);
  writeFileSync(file, html.replace(/<head([^>]*)>/i, `<head$1><base href="${origin}/">`));
  // Chromium refuses a singleton socket under a long TMPDIR, so the probe gets a short one.
  const probe = execFileSync('node', [join(gate, 'design_probe.mjs'), `file://${file}`, '390', '1440'],
    { encoding: 'utf8', timeout: 120_000, env: { ...process.env, TMPDIR: '/tmp' } });
  const lines = join(out, `${name}.jsonl`);
  writeFileSync(lines, probe);
  let text = '';
  try {
    text = execFileSync('python3', [join(gate, 'design_gate.py'), '--rules', lines, '--report'], { encoding: 'utf8' });
  } catch (e: any) {
    text = String(e.stdout ?? '');
  }
  const refusals = text.split('\n').filter((l) => l.startsWith(' - '));
  console.log(`== ${name}: ${refusals.length} refusal(s)\n${text}`);
  return { refusals, text };
}

test('the generated pages the frame draws, judged by the loop design gate itself', async ({ browser, baseURL }) => {
  test.skip(!gate || !out, 'DESIGN_GATE_DIR and DESIGN_GATE_OUT name the gate and where its dumps go');
  test.setTimeout(300_000);
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  await signIn(page);

  await page.goto('/app/task/tasks/new');
  await page.getByLabel('Title').fill('a'.repeat(152));
  await page.getByLabel('Priority').selectOption('high');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);
  const record = new URL(page.url()).pathname;

  await page.goto('/app/user/users');
  const someone = await page.locator('table a[href^="/app/user/users/"]').first().getAttribute('href');
  expect(someone, 'the user list links to no record').toBeTruthy();

  const pages: Array<[string, string]> = [
    ['tasks-list', '/app/task/tasks'],
    ['task-record-unbreakable', record],
    ['users-list', '/app/user/users'],
    ['user-record', someone as string],
  ];
  const origin = baseURL ?? 'http://localhost:8099';
  const tally: Record<string, number> = {};
  for (const [name, path] of pages) {
    await page.goto(path);
    tally[name] = judge(name, await page.content(), origin).refusals.length;
  }
  // Anonymous pages the shared Heading reaches.
  const anonymous = await (await browser.newContext()).newPage();
  for (const [name, path] of [['sign-in', '/app/admin/login'], ['public-home', '/']] as const) {
    const res = await anonymous.goto(path);
    if (res && res.ok()) tally[name] = judge(name, await anonymous.content(), origin).refusals.length;
  }
  console.log(`DESIGN GATE TALLY ${JSON.stringify(tally)}`);
  expect(Object.keys(tally).length, 'the gate measured no page').toBeGreaterThanOrEqual(4);
});
