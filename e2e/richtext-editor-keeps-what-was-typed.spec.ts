import { expect, test, type Page } from '@playwright/test';
import { signInForContent } from './steps/content';

// The rich text editor's first promise is "nothing lost": what a person types is
// what the record stores, the field offers one editing surface for one value, a
// keystroke raises no uncaught error, and a draft survives leaving the page.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const editable = '[data-richtext-surface] [contenteditable="true"]';

async function openNewContent(page: Page, slug: string, title: string) {
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Address name').fill(slug);
  await page.getByLabel('Title').fill(title);
  const surface = page.locator(editable);
  await expect(surface).toBeVisible();
  return surface;
}

async function storedBody(page: Page) {
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').at(-1);
  const read = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(read.status(), await read.text()).toBe(200);
  return (await read.json()).body as string;
}

test.beforeEach(async ({ page }) => {
  await signInForContent(page, email, password);
});

test('typing into the editor raises no uncaught error and reaches the stored record', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const surface = await openNewContent(page, `typed-${Date.now()}`, 'Typed');
  await surface.click();
  await page.keyboard.type('The opening hours changed on Monday.');
  await expect(surface).toContainText('The opening hours changed on Monday.');
  expect(errors, 'a keystroke in the editor must not throw').toEqual([]);
  await page.getByRole('button', { name: 'Save' }).click();
  expect(await storedBody(page)).toContain('The opening hours changed on Monday.');
});

test('the mounted field offers one editing surface: the textarea is hidden behind the editor', async ({ page }) => {
  await openNewContent(page, `single-${Date.now()}`, 'One surface');
  await expect(page.getByLabel('Body')).toBeHidden();
});

test('the character counter follows what is typed', async ({ page }) => {
  const surface = await openNewContent(page, `count-${Date.now()}`, 'Counter');
  await surface.click();
  await page.keyboard.type('Four words were typed');
  await expect(page.locator('[data-richtext-count]')).toContainText('4 words');
});

test('an unsaved draft is offered back when the author returns to the record', async ({ page }) => {
  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug: `draft-${Date.now()}`, title: 'Draft', kind: 'page', body: 'Saved text.' },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  await page.goto(`/app/content/contents/${id}/edit`);
  const surface = page.locator(editable);
  await expect(surface).toBeVisible();
  await surface.click();
  await page.keyboard.press('End');
  await page.keyboard.type(' Unsaved words.');
  // The draft is written two seconds after the last keystroke.
  await page.waitForTimeout(3000);
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(page.locator(editable)).toBeVisible();
  await expect(page.locator('[data-richtext-draft-bar]')).toBeVisible();
  await page.getByRole('button', { name: 'Restore unsaved draft' }).click();
  await expect(page.locator(editable)).toContainText('Unsaved words.');
});
