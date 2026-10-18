import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm, pickRichTextImage } from './steps/richtext';

test('a picked image displays its uploaded pixels while the submitted value keeps the file reference', async ({ page }) => {
  await openRichTextForm(page);
  const id = await pickRichTextImage(page);
  const content = await page.request.get(`/api/v1/file/files/${id}/content`);
  expect(content.status(), await content.text()).toBe(200);
  expect(content.headers()['content-type']).toMatch(/^image\//);
  const image = page.locator(`${editingSurface} img`);
  await expect(image).toHaveCount(1);
  const submitted = page.locator('[data-richtext] textarea');
  await expect(submitted).toHaveValue(/!\[.*\]\(pk-file:[0-9a-f-]{36}/);
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth), {
    message: 'the editor must resolve pk-file to readable pixels, without changing the stored Markdown reference',
  }).toBeGreaterThan(0);
});
