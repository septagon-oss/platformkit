import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm, pickRichTextImage } from './steps/richtext';

test('editing text before a pending image prompt preserves the image description target', async ({ page }) => {
  await openRichTextForm(page);
  await page.keyboard.type('Words before the picture.');
  await page.keyboard.press('Enter');
  const id = await pickRichTextImage(page);
  const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
  await expect(prompt).toBeVisible();
  await page.locator(editingSurface).click();
  await page.keyboard.press('Control+Home');
  await page.keyboard.type('An added introduction. ');
  await expect(page.locator(editingSurface)).toContainText('An added introduction.');
  await prompt.getByRole('textbox', { name: 'Alt text', exact: true }).fill('The uploaded picture');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(prompt).toBeHidden();
  const markdown = await page.locator('[data-richtext] textarea').inputValue();
  expect(markdown).toContain('An added introduction.');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const record = page.url().split('/').at(-1);
  const read = await page.request.get(`/api/v1/content/contents/${record}`);
  expect(read.status()).toBe(200);
  const body = (await read.json()).body;
  console.log(`saved image after editing before its prompt: ${body}`);
  expect(body, 'the pending prompt must follow its image through document edits').toContain(`![The uploaded picture](pk-file:${id}`);
});
