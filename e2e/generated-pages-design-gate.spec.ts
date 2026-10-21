import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

// The loop's own design gate, run on the framed pages it cannot reach by URL: each page is fetched
// behind the administrator's cookie, written to disk with a <base> that points its sheet back at
// the live server, measured by the gate's own probe, and judged by the gate's own rules
// (`design_gate.py --rules`). Nothing here re-implements a rule; the counts printed are the gate's.
// The anonymous pages are judged by URL, as the gate judges any public page: a dump on disk cannot
// fetch its sheet, so only the live probe judges the sheet the framed pages share with sign-in.
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

type Verdict = { refusals: string[]; text: string };

// Chromium refuses a singleton socket under a long TMPDIR, so the probe gets a short one.
const shortTmp = { ...process.env, TMPDIR: '/tmp' };

function judge(name: string, html: string, origin: string): Verdict {
  mkdirSync(out, { recursive: true });
  const file = join(out, `${name}.html`);
  writeFileSync(file, html.replace(/<head([^>]*)>/i, `<head$1><base href="${origin}/">`));
  const probe = execFileSync('node', [join(gate, 'design_probe.mjs'), `file://${file}`, '390', '1440'],
    { encoding: 'utf8', timeout: 120_000, env: shortTmp });
  const lines = join(out, `${name}.jsonl`);
  writeFileSync(lines, probe);
  return verdict(name, ['--rules', lines]);
}

/** The gate's report for one page; a refusal is a line the gate starts with " - ". */
function verdict(name: string, args: string[]): Verdict {
  let text = '';
  try {
    text = execFileSync('python3', [join(gate, 'design_gate.py'), ...args, '--report'],
      { encoding: 'utf8', timeout: 300_000, env: shortTmp });
  } catch (e: any) {
    text = String(e.stdout ?? '');
  }
  const refusals = text.split('\n').filter((l) => l.startsWith(' - '));
  console.log(`== ${name}: ${refusals.length} refusal(s)\n${text}`);
  return { refusals, text };
}

/** Holds one page to the gate: no refusal, and a report that measured both widths and said ok. */
function expectPasses(name: string, v: Verdict) {
  expect(v.refusals, `${name} fails the design gate:\n${v.text}`).toEqual([]);
  expect(v.text, `the gate did not judge ${name} at both widths:\n${v.text}`).toContain('design gate: ok (2 width(s))');
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

  // A page past the last is the list's empty state, whatever rows other specs wrote.
  const pages: Array<[string, string]> = [
    ['tasks-list', '/app/task/tasks'],
    ['task-record-unbreakable', record],
    ['task-form', '/app/task/tasks/new'],
    ['tasks-empty', '/app/task/tasks?page=9999'],
    ['users-list', '/app/user/users'],
    ['user-record', someone as string],
  ];
  const origin = baseURL ?? 'http://localhost:8099';
  const verdicts: Record<string, Verdict> = {};
  for (const [name, path] of pages) {
    await page.goto(path);
    verdicts[name] = judge(name, await page.content(), origin);
  }
  // Anonymous pages the shared Heading reaches, probed live so their sheets are read and judged.
  const anonymous = await (await browser.newContext()).newPage();
  for (const [name, path] of [['sign-in', '/app/admin/login'], ['public-home', '/']] as const) {
    const res = await anonymous.goto(path);
    expect(res?.ok(), `${path} answered ${res?.status()}`).toBe(true);
    verdicts[name] = verdict(name, [`${origin}${path}`, '--widths', '390,1440']);
    expect(verdicts[name].text, `the gate could not read a sheet ${path} serves:\n${verdicts[name].text}`).not.toContain('could not read');
  }
  const tally = Object.fromEntries(Object.entries(verdicts).map(([name, v]) => [name, v.refusals.length]));
  console.log(`DESIGN GATE TALLY ${JSON.stringify(tally)}`);
  expect(Object.keys(tally).length, 'the gate measured no page').toBeGreaterThanOrEqual(4);
  for (const [name, v] of Object.entries(verdicts)) expectPasses(name, v);
});
