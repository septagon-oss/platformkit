import { expect, test } from '@playwright/test';
import { openRichTextForm, pickRichTextImage } from './steps/richtext';

test('an informative image requires alt text before its insertion prompt can complete', async ({ page }) => {
  await openRichTextForm(page);
  await pickRichTextImage(page);
  const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
  await expect(prompt).toBeVisible();
  const input = prompt.locator('input[type="text"]');
  await expect(input).toHaveValue('');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(prompt, 'empty alt text is refused until Decorative is chosen').toBeVisible();
  await input.fill('A red picture');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(prompt).toBeHidden();
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/!\[A red picture\]\(pk-file:/);
});
