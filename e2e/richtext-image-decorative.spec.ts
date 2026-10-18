import { expect, test } from '@playwright/test';
import { openRichTextForm, pickRichTextImage } from './steps/richtext';

test('choosing Decorative completes image insertion with an empty alt attribute', async ({ page }) => {
  await openRichTextForm(page);
  const id = await pickRichTextImage(page);
  const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
  await expect(prompt).toBeVisible();
  const alt = prompt.getByRole('textbox', { name: 'Alt text', exact: true });
  await expect(alt).toHaveValue('');
  await prompt.getByRole('checkbox', { name: 'Decorative', exact: true }).check();
  await expect(alt).not.toHaveAttribute('required');
  await prompt.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(prompt, 'Decorative is the explicit alternative to a required description').toBeHidden();
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(new RegExp(`!\\[\\]\\(pk-file:${id}`));
  await expect(page.locator('[data-richtext-surface] img')).toHaveAttribute('alt', '');
});
