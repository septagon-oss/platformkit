import { expect, test } from '@playwright/test';
import { editingSurface, imageBytes, openRichTextForm } from './steps/richtext';

test('saved images load and keep their descriptions when surrounding text is edited', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await openRichTextForm(page);
  const images = [];
  for (const name of ['first', 'second']) {
    const uploaded = await page.request.post('/api/v1/file/files?image=true', {
      multipart: { file: { name: `${name}.png`, mimeType: 'image/png', buffer: imageBytes } },
    });
    expect(uploaded.status(), await uploaded.text()).toBe(201);
    const { id } = await uploaded.json();
    images.push({ id, alt: `${name} picture`, caption: `${name} caption` });
  }
  const paragraphs = images.map(({ id, alt, caption }) => `![${alt}](pk-file:${id} "${caption}")`);
  const created = await page.request.post('/api/v1/content/contents', {
    data: {
      slug: `editable-saved-images-${Date.now()}`,
      title: 'Saved pictures and text',
      kind: 'page',
      body: ['Before the pictures.', ...paragraphs, 'After the pictures.'].join('\n\n') + '\n',
    },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id, body: original } = await created.json();
  await page.goto(`/app/content/contents/${id}/edit`);
  const surface = page.locator(editingSurface);
  await expect(surface).toBeVisible();
  await expect(surface.locator('img')).toHaveCount(2);
  for (const image of images) {
    const picture = surface.locator(`img[alt="${image.alt}"]`);
    await expect(picture).toHaveAttribute('title', image.caption);
    await expect(picture).toHaveAttribute('src', new RegExp(`/file/files/${image.id}/content$`));
    await expect.poll(() => picture.evaluate((node: HTMLImageElement) => node.complete && node.naturalWidth > 0)).toBe(true);
  }
  await surface.click();
  await surface.press('Control+End');
  await surface.pressSequentially(' More writing.');
  await expect(page.locator('textarea[name="body"]')).toHaveValue(/After the pictures\. More writing\./);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/app/content/contents/${id}$`));
  const after = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(after.status(), await after.text()).toBe(200);
  const { body } = await after.json();
  expect(body).toBe(original.replace('After the pictures.', 'After the pictures. More writing.'));
  for (const paragraph of paragraphs) expect(body).toContain(paragraph);
  expect(body).toContain('After the pictures. More writing.');
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(surface).toBeVisible();
  await expect(surface.locator('img')).toHaveCount(2);
  await expect(surface).toContainText('After the pictures. More writing.');
  expect(errors).toEqual([]);
});
