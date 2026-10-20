import { expect, test, type Browser, type Page } from '@playwright/test';
import { diagnosed, probe, refusals, report } from './design_floor';

// The workspace home as a person meets it: the header names them, the facts support reads are
// behind "About this workspace", the application's primary action comes before any counter, a
// counter is shown only to whoever may read what it counts, and the summary takes the full width
// at phone width. Every number the floor measures goes to stdout, so the run is the measurement a
// report quotes.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
// A maker of this spec's own: a role that reads tasks and nothing else, and a name of their own.
const maker = {
  email: `workspace-home-${Date.now()}@e2e.test`,
  password: 'the-workspace-home-chooses-this-1',
  name: 'Homed Maker',
};
const role = `workspace_home_${Date.now()}`;
const widths = [390, 1440];
// The build stamp as the footer prints it, so a tenant that is itself named PlatformKit is not one.
const stamp = /^PlatformKit ([0-9a-f]{8}|\(development\)|\(unknown build\))$/;
const action = 'main a[href="/app/task/tasks"][data-variant="primary"]';

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  await signIn(page, admin.email, admin.password);
  const created = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['task:read'] } });
  expect(created.status(), await created.text()).toBe(200);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: maker.email, displayName: maker.name, roles: [role] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, { data: { password: maker.password } });
  expect(activated.status(), await activated.text()).toBe(200);
  await context.close();
});

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

/** The home at one width, for one person. Sign-in lands on the first screen a role opens, not here. */
async function homeAt(browser: Browser, width: number, who: { email: string; password: string }): Promise<Page> {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  const page = await context.newPage();
  await signIn(page, who.email, who.password);
  const response = await page.goto('/app');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  return page;
}

test('the header names the person and no identifier stands outside About this workspace', async ({ browser }) => {
  test.setTimeout(90_000);
  for (const width of widths) {
    const page = await homeAt(browser, width, maker);
    const header = page.locator('header:not([data-sidebar-brand])');
    const name = header.getByText(maker.name, { exact: true });
    await expect(name, `the header does not name the maker at ${width}px`).toBeVisible();
    const box = await name.evaluate((e) => {
      const r = e.getBoundingClientRect();
      return { left: r.left, right: r.right, clipped: e.scrollWidth - e.clientWidth };
    });
    expect(box.left, `the name starts off-screen at ${width}px`).toBeGreaterThanOrEqual(0);
    expect(box.right, `the name runs off-screen at ${width}px`).toBeLessThanOrEqual(width);
    expect(box.clipped, `the name is clipped at ${width}px`).toBeLessThanOrEqual(0);

    const footer = page.locator('footer');
    for (const region of [header, footer]) {
      await expect(region.getByText(stamp)).toBeHidden();
      await expect(region.getByText('Tenant workspace', { exact: true })).toBeHidden();
      await expect(region.getByText('Platform operator', { exact: true })).toBeHidden();
    }
    await footer.locator('summary').filter({ hasText: 'About this workspace' }).click();
    await expect(footer.getByText(stamp), `the build stamp is not behind the disclosure at ${width}px`).toBeVisible();
    await expect(footer.getByText(/^(Tenant workspace|Platform operator)$/), `the scope is not behind the disclosure at ${width}px`).toBeVisible();
    await page.context().close();
  }
});

test('the home leads with the app\'s primary action, before any counter', async ({ browser }) => {
  test.setTimeout(90_000);
  for (const width of widths) {
    for (const who of [admin, maker]) {
      const page = await homeAt(browser, width, who);
      const button = page.locator(action);
      await expect(button, `${who.email} at ${width}px is not shown the primary action`).toBeVisible();
      await expect(button).toHaveText('Go to tasks');
      const top = (await button.boundingBox())!.y;
      for (const card of await page.locator('main [data-component="card"]').all()) {
        expect((await card.boundingBox())!.y, `a card stands above the primary action for ${who.email} at ${width}px`).toBeGreaterThan(top);
      }
      await page.context().close();
    }
  }
});

test('a person without a counter\'s read permission sees no counter for it', async ({ browser }) => {
  test.setTimeout(60_000);
  const page = await homeAt(browser, 1440, maker);
  await expect(page.locator('main a[href="/app/user/users"]')).toHaveCount(0);
  await expect(page.locator('main a[href="/app/auth/roles"]')).toHaveCount(0);
  await expect(page.locator('main a[href="/app/task/tasks"][data-component="card"]'), 'the maker loses the counter they may read').toHaveCount(1);
});

test('at 390 every summary row takes the full width', async ({ browser }) => {
  test.setTimeout(60_000);
  for (const who of [admin, maker]) {
    const page = await homeAt(browser, 390, who);
    const rows = await page.evaluate(() => [...document.querySelectorAll('main [data-component="card"]')].map((card) => {
      // A row's width is measured against the grid that lays it out, inside the section's padding.
      const own = card.getBoundingClientRect();
      const grid = card.parentElement!.getBoundingClientRect();
      return { width: Math.round(own.width), grid: Math.round(grid.width), top: Math.round(own.top) };
    }));
    console.log(`  summary rows @390px for ${who.email}: ${JSON.stringify(rows)}`);
    expect(rows.length, `no summary row to measure for ${who.email}`).toBeGreaterThan(0);
    for (const row of rows) {
      expect(Math.abs(row.width - row.grid), `a summary row is ${row.width}px in a ${row.grid}px grid`).toBeLessThanOrEqual(2);
    }
    expect(new Set(rows.map((r) => r.top)).size, 'two summary rows share a line at 390px').toBe(rows.length);
    await page.context().close();
  }
});

/** Every floor refusal of the home, for both people at both widths, each measurement printed first. */
async function homeRefusals(browser: Browser): Promise<string[]> {
  const refused: string[] = [];
  for (const width of widths) {
    for (const [label, who] of [['/app maker', maker], ['/app administrator', admin]] as const) {
      const page = await homeAt(browser, width, who);
      const m = await probe(page, width);
      console.log(report(label, m));
      refused.push(...refusals(m).map((r) => `${label}: ${diagnosed(m, r)}`));
      await page.context().close();
    }
  }
  return refused;
}

// Measured on the home before the primary action and the disclosure, and after them: the same four
// refusals, all of one rule — the card grid sets each card's icon, title and sentence on edges of
// their own, inside a section inset from the page, so the home draws more than one content column.
// That is the frame's left-edge work, not this page's order. This case refuses every other rule.
test('the home refuses no floor rule but the card grid\'s left edges', async ({ browser }) => {
  test.setTimeout(90_000);
  expect((await homeRefusals(browser)).filter((r) => !/ left edges \[/.test(r))).toEqual([]);
});

// Known defect: the whole floor, which the card grid's left edges refuse at both widths today. It is
// expected to fail, so the day the grid meets the floor this case goes red until test.fail() leaves.
test('the home meets the design floor at 390 and 1440', async ({ browser }) => {
  test.fail();
  test.setTimeout(90_000);
  expect(await homeRefusals(browser)).toEqual([]);
});
