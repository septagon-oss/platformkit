import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm, pickRichTextImage } from './steps/richtext';

test('the alt prompt changes the newly inserted image and preserves the first image description', async ({ page }) => {
  await openRichTextForm(page);
  const first = await pickRichTextImage(page, 'first.png');
  const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
  await prompt.locator('input[type="text"]').fill('The first picture');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  await page.locator(editingSurface).click();
  await page.keyboard.press('Control+End');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Between the pictures.');
  await page.keyboard.press('Enter');
  const second = await pickRichTextImage(page, 'second.png');
  await prompt.locator('input[type="text"]').fill('The second picture');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  const value = await page.locator('[data-richtext] textarea').inputValue();
  expect(value).toContain(`![The first picture](pk-file:${first}`);
  expect(value).toContain(`![The second picture](pk-file:${second}`);
});
