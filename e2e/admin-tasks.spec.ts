import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Gate 10. One spec, and it is the round trip a person makes on their first
// day: sign in, create a task through the generated form, find it in the
// generated list, change it, and delete it.
//
// The task supplies representative text, select and nullable date fields. The
// journey checks the generated controls and persisted edits, not its lifecycle.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const title = `Chiller supply temperature out of band ${Date.now()}`;
const renamed = `${title} (resolved)`;

test.beforeEach(async ({ page }) => {
  await page.goto('/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/admin$/);
});

test('the admin shell renders and a generated CRUD screen works', async ({ page }) => {
  // The dashboard is the shell: navigation the caller may follow, and a card
  // per resource with the count its own list route would report.
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await page.getByRole('link', { name: 'Tasks' }).first().click();
  await expect(page).toHaveURL(/\/admin\/task\/tasks$/);
  await expect(page.getByRole('heading', { name: 'Tasks' })).toBeVisible();

  // Create. The form is generated from the schema: a select exists because the
  // struct says enum, and the title is required because it says validate.
  await page.getByRole('link', { name: 'New task' }).click();
  await expect(page).toHaveURL(/\/admin\/task\/tasks\/new$/);
  await page.getByLabel('Title').fill(title);
  await page.getByLabel('Priority').selectOption('high');
  await page.getByLabel('Description').fill('Inspect the supply hose');
  await page.getByLabel('Due At').fill('2026-12-01T14:30');
  await page.getByRole('button', { name: 'Save' }).click();

  // The write redirects to the row it created.
  await expect(page).toHaveURL(/\/admin\/task\/tasks\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('heading', { name: title })).toBeVisible();
  const row = page.url();

  // It is in the list.
  await page.goto('/admin/task/tasks');
  await expect(page.getByRole('link', { name: title })).toBeVisible();

  // Edit.
  await page.goto(`${row}/edit`);
  await expect(page.getByLabel('Description')).toHaveValue('Inspect the supply hose');
  await expect(page.getByLabel('Due At')).not.toHaveValue('');
  await page.getByLabel('Title').fill(renamed);
  await page.getByLabel('Description').fill('');
  await page.getByLabel('Due At').fill('');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(row);
  await expect(page.getByRole('heading', { name: renamed })).toBeVisible();
  await page.goto(`${row}/edit`);
  await expect(page.getByLabel('Description')).toHaveValue('');
  await expect(page.getByLabel('Due At')).toHaveValue('');
  await page.goto(row);

  // Delete, through the confirm dialog the shell puts on every page.
  await page.getByRole('button', { name: 'Delete' }).click();
  const dialog = page.locator('#pk-confirm');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(page).toHaveURL(/\/admin\/task\/tasks$/);
  await expect(page.getByRole('link', { name: renamed })).toHaveCount(0);
});

test('an empty title is refused on the form rather than by a page of JSON', async ({ page }) => {
  await page.goto('/admin/task/tasks/new');
  await page.getByRole('button', { name: 'Save' }).click();
  // The browser's own required check fires first, which is the point of
  // rendering it: the request is never made.
  await expect(page.getByLabel('Title')).toBeFocused();
});

test('the newly bootstrapped operator can inspect delivery and refresh by keyboard', async ({ page }) => {
  await page.getByRole('link', { name: 'Event delivery', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/delivery$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Event delivery' })).toBeVisible();
  await expect(page.getByRole('main')).toHaveCount(1);
  await expect(page.getByText(/^Observed at /)).toContainText('Records can change between reads.');
  for (const width of [1280, 320]) {
    await page.setViewportSize({ width, height: 900 });
    for (const name of ['Pending publication', 'Terminal failures']) {
      const region = page.getByRole('region', { name, exact: true });
      await expect(region.getByRole('table')).toBeVisible();
      await region.focus();
      await expect(region).toBeFocused();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
    await page.screenshot({ path: test.info().outputPath(`delivery-${width}.png`), fullPage: true });
    const refresh = page.getByRole('link', { name: 'Refresh records', exact: true });
    await refresh.focus();
    await expect(refresh).toBeFocused();
    const response = page.waitForResponse(r => new URL(r.url()).pathname === '/admin/delivery');
    await page.keyboard.press('Enter');
    expect((await response).status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: 'Event delivery' })).toBeVisible();
  }
  const direct = await page.request.get('/admin/delivery');
  expect(direct.status()).toBe(200);
  expect(direct.headers()['cache-control']).toContain('no-store');
  await expect(page.getByRole('button', { name: /retry|replay|delete/i })).toHaveCount(0);

  // Change only the disposable fixture's existing role, preserving every other
  // grant. The same authenticated operator must lose both the link and access.
  expect(process.env.PLATFORMKIT_E2E_FIXTURE_DATABASE).toMatch(/^platformkit_e2e_\d+_\d+_\d+$/);
  expect(new URL(page.url()).hostname).toBe('localhost');
  const roles = await page.request.get('/api/v1/auth/roles');
  expect(roles.status()).toBe(200);
  const operator = (await roles.json()).items.find((role: { name: string }) => role.name === 'admin');
  const permissions: string[] = operator.permissions;
  expect(permissions).toContain('delivery:read');
  expect(permissions).toContain('*');
  try {
    expect((await page.request.put('/api/v1/auth/roles/admin', {
      data: { permissions: permissions.filter(value => value !== 'delivery:read') },
    })).status()).toBe(200);
    expect((await page.request.get('/admin/delivery')).status()).toBe(403);
    await page.goto('/admin');
    await expect(page.getByRole('link', { name: 'Event delivery', exact: true })).toHaveCount(0);
  } finally {
    expect((await page.request.put('/api/v1/auth/roles/admin', { data: { permissions } })).status()).toBe(200);
  }
  expect((await page.request.get('/admin/delivery')).status()).toBe(200);
});

test('a refused generated edit retains command-owned values for the retry', async ({ page }) => {
  const created = await page.request.post('/api/v1/task/tasks', {
    data: { title: 'Keep the assigned technician during a refused edit' },
  });
  expect(created.status()).toBe(201);
  const task = await created.json();
  const identity = await page.request.get('/api/v1/auth/me');
  expect(identity.status()).toBe(200);
  const assigneeId = (await identity.json()).userId;
  const taskPath = `/api/v1/task/tasks/${task.id}`;
  expect((await page.request.post(`${taskPath}/assign`, { data: { assigneeId } })).status()).toBe(200);

  await page.goto(`/admin/task/tasks/${task.id}/edit`);
  const assignee = page.locator('[name="assigneeId"]');
  await expect(assignee).toHaveJSProperty('readOnly', true);
  await expect(assignee).toHaveValue(assigneeId);
  // Whitespace passes the browser's required control but fails the entity's rule.
  await page.getByLabel('Title').fill('   ');
  const rejected = page.waitForResponse(response =>
    new URL(response.url()).pathname === `/admin/task/tasks/${task.id}` && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Save' }).click();
  expect((await rejected).status()).toBe(422);
  await expect(page.getByRole('alert').first()).toContainText('a task needs a title');
  await expect(assignee).toHaveValue(assigneeId);
  await expect(assignee).toHaveJSProperty('readOnly', true);

  await page.getByLabel('Title').fill('Corrected title keeps its assignment');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/task/tasks/${task.id}$`));
  const saved = await page.request.get(taskPath);
  expect(saved.status()).toBe(200);
  expect(await saved.json()).toMatchObject({ title: 'Corrected title keeps its assignment', assigneeId });
});

test('the typed gallery retains native controls at desktop and narrow widths', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  for (const width of [1280, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/_gallery/preview?example=pk-ui.component.button/primary');
    const primary = page.getByRole('button', { name: 'Save', exact: true });
    await expect(primary).toBeVisible();
    await expect(primary).toHaveAttribute('type', 'button');
    await page.goto('/admin/_gallery/preview?example=pk-ui.component.input/email');
    const emailField = page.getByRole('textbox', { name: /Email/ });
    await expect(emailField).toBeVisible();
    await expect(emailField).toHaveAttribute('type', 'email');
  }
  await page.goto('/admin/_gallery/preview?example=pk-ui.component.button/with-icon');
  await expect(page.locator('button svg')).toHaveAttribute('aria-hidden', 'true');
  expect(errors).toEqual([]);
});

test('a disabled gallery link refuses keyboard, pointer and HTMX activation', async ({ page }) => {
  await page.goto('/admin/_gallery/preview?example=pk-ui.component.button/disabled-link');
  expect(await page.evaluate(() => 'htmx' in window)).toBe(true);
  const disabled = page.getByRole('link', { name: 'Unavailable', exact: true });
  await expect(disabled).toHaveAttribute('role', 'link');
  await expect(disabled).toHaveAttribute('aria-disabled', 'true');
  await expect(disabled).toHaveAttribute('tabindex', '-1');
  await expect(disabled).toHaveAttribute('hx-disable', 'true');
  for (const attribute of ['href', 'hx-get', 'hx-boost']) {
    await expect(disabled).not.toHaveAttribute(attribute);
  }

  const adminRequests: string[] = [];
  page.on('request', request => {
    if (new URL(request.url()).pathname === '/admin') {
      adminRequests.push(request.method());
    }
  });
  // tabindex=-1 still allows focus(), and pointer-events does not block Enter.
  await disabled.focus();
  await expect(disabled).toBeFocused();
  await page.keyboard.press('Enter');

  // locator.click() refuses aria-disabled controls instead of exercising them.
  await disabled.scrollIntoViewIfNeeded();
  const bounds = await disabled.boundingBox();
  if (!bounds) throw new Error('The disabled gallery link has no pointer target');
  await page.mouse.click(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2);
  // Bypass pointer hit-testing too: HTMX must not handle a direct DOM activation.
  await disabled.evaluate(node => (node as HTMLElement).click());

  // Complete an enabled navigation before inspecting requests. The structural
  // guards above, not an immediate URL check or a sleep, establish inactivity.
  await page.goto('/admin/_gallery/preview?example=pk-ui.component.button/as-link');
  const enabled = page.getByRole('link', { name: 'Open', exact: true });
  await expect(enabled).toHaveAttribute('href', '/somewhere');
  await enabled.evaluate(node => (node as HTMLElement).click());
  await expect(page).toHaveURL(/\/somewhere$/);
  expect(adminRequests).toEqual([]);
});

// The theme is the one piece of state this application keeps in the browser,
// and a default is not a piece of state. theme.js used to apply-and-store on
// every load, so a person who never touched the toggle came back to a
// remembered choice they had not made — and an installation shipping a dark
// palette of its own was overridden by a stored "light" nobody chose.
//
// localStorage is replaced here rather than read, because the claim is about
// what is written and "nothing was written" cannot be observed from what is
// there.
test('a page stores no theme until somebody chooses one', async ({ page }) => {
  await page.addInitScript(() => {
    (window as unknown as { __writes: string[] }).__writes = [];
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        length: 0,
        key: () => null,
        getItem: () => null,
        removeItem: () => {},
        clear: () => {},
        setItem: (k: string, v: string) => {
          (window as unknown as { __writes: string[] }).__writes.push(`${k}=${v}`);
        },
      },
    });
  });
  const written = () => page.evaluate(() => (window as unknown as { __writes: string[] }).__writes);

  await page.goto('/admin');
  expect(await page.evaluate(() => document.documentElement.hasAttribute('data-theme'))).toBe(false);
  expect(await written()).toEqual([]);

  // A click is a choice: it applies, and only then is it remembered.
  await page.getByRole('button', { name: 'Switch between the light and dark theme' }).click();
  expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toMatch(/^(light|dark)$/);
  expect(await written()).toHaveLength(1);
});
