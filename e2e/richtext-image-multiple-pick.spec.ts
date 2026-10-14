import { expect, test } from '@playwright/test';
import { imageBytes, openRichTextForm } from './steps/richtext';

test('picking two images retains both uploaded references and their own descriptions', async ({ page }) => {
  await openRichTextForm(page);
  const uploaded: string[] = [];
  page.on('response', async (response) => {
    if (response.request().method() !== 'POST' || !response.url().includes('/api/v1/file/files')) return;
    if (response.status() === 201) uploaded.push((await response.json()).id);
  });
  await page.locator('[data-richtext-file]').setInputFiles([
    { name: 'one.png', mimeType: 'image/png', buffer: imageBytes },
    { name: 'two.png', mimeType: 'image/png', buffer: imageBytes },
  ]);
  await expect.poll(() => uploaded.length).toBe(2);
  const prompts = page.getByRole('group', { name: 'Alt text', exact: true });
  await expect(prompts).toHaveCount(2);
  await prompts.nth(0).getByRole('textbox', { name: 'Alt text', exact: true }).fill('First uploaded picture');
  await prompts.nth(1).getByRole('textbox', { name: 'Alt text', exact: true }).fill('Second uploaded picture');
  await prompts.nth(0).getByRole('button', { name: 'Save', exact: true }).click();
  await prompts.nth(0).getByRole('button', { name: 'Save', exact: true }).click();
  await expect(prompts).toHaveCount(0);
  const markdown = await page.locator('[data-richtext] textarea').inputValue();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const record = page.url().split('/').at(-1);
  const read = await page.request.get(`/api/v1/content/contents/${record}`);
  expect(read.status()).toBe(200);
  const body = (await read.json()).body;
  console.log(`two picked images submitted: ${markdown}; stored: ${body}`);
  for (const id of uploaded) expect(body, 'each successful upload must remain in the saved document').toContain(`pk-file:${id}`);
  expect(body).toContain('![First uploaded picture]');
  expect(body).toContain('![Second uploaded picture]');
});
