import { expect, test } from '@playwright/test';

// Gate 10. One spec, and it is the round trip a person makes on their first
// day: sign in, create a task through the generated form, find it in the
// generated list, change it, and delete it.
//
// Nothing here knows what a task is beyond its title. The screens are derived
// from the entity's schema, so a spec that reached for a field this module
// happens to declare would be testing modules/task rather than stage E4.

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
  await page.getByLabel('Title').fill(renamed);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(row);
  await expect(page.getByRole('heading', { name: renamed })).toBeVisible();

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

test('the typed gallery retains native controls at desktop and narrow widths', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/admin/_gallery');
  const primary = page.locator('[data-gallery-example="Button / primary"] button');
  const emailField = page.locator('[data-gallery-example="Input / email"] input');
  for (const width of [1280, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(primary).toBeVisible();
    await expect(primary).toHaveAccessibleName('Save');
    await expect(primary).toHaveAttribute('type', 'button');
    await expect(emailField).toHaveAccessibleName(/Email/);
    await expect(emailField).toHaveAttribute('type', 'email');
  }
  await expect(page.locator('[data-gallery-example="Button / with icon"] svg')).toHaveAttribute('aria-hidden', 'true');
  expect(errors).toEqual([]);
});

test('a disabled gallery link refuses keyboard, pointer and HTMX activation', async ({ page }) => {
  await page.goto('/admin/_gallery');
  expect(await page.evaluate(() => 'htmx' in window)).toBe(true);
  const disabled = page.locator('[data-gallery-example="Button / disabled link"]')
    .getByRole('link', { name: 'Unavailable', exact: true });
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
  const enabled = page.locator('[data-gallery-example="Button / as link"]')
    .getByRole('link', { name: 'Open', exact: true });
  await expect(enabled).toHaveAttribute('href', '/somewhere');
  await enabled.click();
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
