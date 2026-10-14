import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm, pickRichTextImage } from './steps/richtext';

test('a saved image reopens in the rich editor and survives an untouched save', async ({ page }) => {
  page.on('pageerror', (error) => console.log('editor error: ' + error.message));
  await openRichTextForm(page);
  const file = await pickRichTextImage(page);
  const caption = 'A picture caption';
  const escaped = caption.replace(/"/g, '\\"');
  const created = await page.request.post('/api/v1/content/contents', {
    data: {
      slug: `reopened-image-${Date.now()}`,
      title: 'An image with a caption',
      kind: 'page',
      body: `![The picture](pk-file:${file} "${escaped}")\n`,
    },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id, body: original } = await created.json();
  console.log(`saved image before reopening: ${original}`);
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(page.locator(editingSurface)).toBeVisible();
  await expect(page.locator(`${editingSurface} img`)).toHaveAttribute('alt', 'The picture');
  await expect(page.locator(`${editingSurface} img`)).toHaveAttribute('title', caption);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/app/content/contents/${id}$`));
  const after = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(after.status()).toBe(200);
  const saved = (await after.json()).body;
  console.log(`image caption before: ${original}; after: ${saved}`);
  expect(saved, 'an untouched save preserves the supported image and its caption').toBe(original);
  await expect(page.locator('[data-component="prose"] img')).toHaveAttribute('alt', 'The picture');
});
