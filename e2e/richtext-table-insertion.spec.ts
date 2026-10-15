import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('the table command inserts an editable table without an uncaught error', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await openRichTextForm(page);
  await page.keyboard.type('/');
  await expect(page.locator('[data-richtext-menu]')).toBeVisible();
  await page.keyboard.type('table');
  await expect(page.locator('[data-richtext-menu] [data-richtext-command="table"]')).toBeVisible();
  await page.keyboard.press('Enter');
  console.log(`table command errors: ${JSON.stringify(errors)}`);
  expect(errors, 'an offered formatting action must not throw').toEqual([]);
  await expect(page.locator(editingSurface).locator('table'), 'Table is an offered editing command').toBeVisible();
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/\|.*\|/);
});
