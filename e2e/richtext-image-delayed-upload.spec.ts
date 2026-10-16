import { expect, test } from '@playwright/test';
import { imageBytes, openRichTextForm } from './steps/richtext';

test('answering the first image description before the second upload finishes retains both picked images', async ({ page }) => {
  await openRichTextForm(page);
  let releaseSecond!: () => void;
  const secondMayFinish = new Promise<void>((resolve) => { releaseSecond = resolve; });
  await page.route('**/api/v1/file/files?image=true', async (route) => {
    if (route.request().postDataBuffer()?.includes(Buffer.from('two.png'))) await secondMayFinish;
    await route.continue();
  });
  const uploaded: string[] = [];
  page.on('response', async (response) => {
    if (response.request().method() === 'POST' && response.url().includes('/api/v1/file/files') && response.status() === 201) {
      uploaded.push((await response.json()).id);
    }
  });
  try {
    await page.locator('[data-richtext-file]').setInputFiles([
      { name: 'one.png', mimeType: 'image/png', buffer: imageBytes },
      { name: 'two.png', mimeType: 'image/png', buffer: imageBytes },
    ]);
    const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
    await expect(prompt).toHaveCount(1);
    await expect.poll(() => uploaded.length).toBe(1);
    await prompt.getByRole('textbox', { name: 'Alt text', exact: true }).fill('First picked image');
    await prompt.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(prompt).toHaveCount(0);
    releaseSecond();
    await expect.poll(() => uploaded.length).toBe(2);
    await expect(prompt).toHaveCount(1);
    await prompt.getByRole('textbox', { name: 'Alt text', exact: true }).fill('Second picked image');
    await prompt.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(prompt).toHaveCount(0);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
    const record = page.url().split('/').at(-1);
    const read = await page.request.get(`/api/v1/content/contents/${record}`);
    expect(read.status()).toBe(200);
    const body = (await read.json()).body;
    console.log(`picked images ${uploaded.join(', ')}; saved body: ${body}`);
    for (const id of uploaded) expect(body, 'both files chosen together survive regardless of upload timing').toContain(`pk-file:${id}`);
    expect(body).toContain('![First picked image]');
    expect(body).toContain('![Second picked image]');
  } finally {
    releaseSecond();
  }
});
