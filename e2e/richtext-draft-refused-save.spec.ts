import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('a refused form save preserves the unsaved draft until a save succeeds', async ({ page }) => {
  await openRichTextForm(page);
  const suffix = Date.now();
  const occupiedSlug = `occupied-${suffix}`;
  const occupied = await page.request.post('/api/v1/content/contents', {
    data: { slug: occupiedSlug, title: 'Existing page', kind: 'page', body: 'Already here.' },
  });
  expect(occupied.status(), await occupied.text()).toBe(201);
  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug: `draft-refusal-${suffix}`, title: 'Draft', kind: 'page', body: 'Saved text.' },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  await page.goto(`/app/content/contents/${id}/edit`);
  const root = page.locator('[data-richtext]');
  await expect(page.locator(editingSurface)).toBeVisible();
  const key = await root.getAttribute('data-richtext-draft');
  expect(key).toBeTruthy();
  await page.locator(editingSurface).click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(' Unsaved words that must survive.');
  await expect.poll(() => page.evaluate((draftKey) => localStorage.getItem(draftKey!), key)).toContain('Unsaved words that must survive.');
  await page.getByLabel('Address name').fill(occupiedSlug);
  const refused = page.waitForResponse((response) => response.request().method() === 'POST' && response.url().includes(`/contents/${id}`));
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const refusal = await refused;
  expect(refusal.status()).toBeGreaterThanOrEqual(400);
  console.log(`refused form save: HTTP ${refusal.status()}`);
  const read = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(read.status()).toBe(200);
  expect((await read.json()).body).toBe('Saved text.\n');
  console.log('the server row is unchanged after refusal');
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(page.locator(editingSurface)).toBeVisible();
  await expect(page.locator('[data-richtext-draft-bar]'), 'refused writes must not clear the only draft').toBeVisible();
  await page.getByRole('button', { name: 'Restore unsaved draft', exact: true }).click();
  await expect(page.locator(editingSurface)).toContainText('Unsaved words that must survive.');
});
