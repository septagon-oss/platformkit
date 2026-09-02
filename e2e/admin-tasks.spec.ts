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
