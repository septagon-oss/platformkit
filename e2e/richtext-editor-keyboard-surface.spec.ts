import { expect, test, type Page } from '@playwright/test';
import { signInForContent } from './steps/content';

// The editor's keyboard surface, as the brief lists it: a `/` on an empty line
// opens the command menu and Enter inserts the chosen block; the Markdown spelling
// of a block becomes that block as it is typed; a selection shows the bubble; a
// paste arrives in the document, and a URL pasted over a selection links it.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const editable = '[data-richtext-surface] [contenteditable="true"]';

async function openNewContent(page: Page, slug: string, title: string) {
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Address name').fill(slug);
  await page.getByLabel('Title').fill(title);
  const surface = page.locator(editable);
  await expect(surface).toBeVisible();
  await surface.click();
  return surface;
}

async function storedBody(page: Page) {
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').at(-1);
  const read = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(read.status(), await read.text()).toBe(200);
  return (await read.json()).body as string;
}

// paste dispatches a clipboard event on the editing surface with the given text,
// the way a browser does when a person presses Ctrl+V.
async function paste(page: Page, text: string) {
  await page.evaluate(({ selector, value }) => {
    const transfer = new DataTransfer();
    transfer.setData('text/plain', value);
    const target = document.querySelector(selector) as HTMLElement;
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }));
  }, { selector: editable, value: text });
}

test.beforeEach(async ({ page }) => {
  await signInForContent(page, email, password);
});

test('a slash on an empty line opens the command menu and Enter inserts the chosen block', async ({ page }) => {
  await openNewContent(page, `slash-${Date.now()}`, 'Slash menu');
  await page.keyboard.type('/');
  const menu = page.locator('[data-richtext-menu]');
  await expect(menu).toBeVisible();
  await page.keyboard.type('quo');
  await expect(menu.getByRole('option', { name: /Quote/ })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(menu).toBeHidden();
  await page.keyboard.type('A passage someone else wrote');
  expect(await storedBody(page)).toMatch(/^> A passage someone else wrote/m);
});

test('the Markdown spelling of a heading becomes a heading as it is typed', async ({ page }) => {
  await openNewContent(page, `heading-${Date.now()}`, 'Shortcut');
  await page.keyboard.type('## Opening hours');
  await expect(page.locator(`${editable} h2`)).toHaveText('Opening hours');
  expect(await storedBody(page)).toMatch(/^## Opening hours$/m);
});

test('selecting text shows the formatting bubble', async ({ page }) => {
  await openNewContent(page, `bubble-${Date.now()}`, 'Bubble');
  await page.keyboard.type('Select these words');
  await page.keyboard.press('Shift+Home');
  await expect(page.locator('[data-richtext-bubble]')).toBeVisible();
});

test('plain text pasted into the editor arrives in the document', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const surface = await openNewContent(page, `paste-${Date.now()}`, 'Paste');
  await paste(page, 'Pasted words');
  expect(errors, 'a paste into the editor must not throw').toEqual([]);
  await expect(surface).toContainText('Pasted words');
});

test('a URL pasted over a selection links the selection', async ({ page }) => {
  await openNewContent(page, `link-${Date.now()}`, 'Paste link');
  await page.keyboard.type('Website');
  await page.keyboard.press('Shift+Home');
  await paste(page, 'https://example.com');
  await expect(page.locator(`${editable} a[href="https://example.com"]`)).toHaveText('Website');
  expect(await storedBody(page)).toContain('[Website](https://example.com)');
});
