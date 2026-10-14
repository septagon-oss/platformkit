import { expect, test } from '@playwright/test';
import { openRichTextForm } from './steps/richtext';

test('a site relative link chosen in the link dialog can be saved with the record', async ({ page }) => {
  await openRichTextForm(page);
  let submitted = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().includes('/content/contents')) submitted++;
  });
  await page.keyboard.type('Read more');
  await page.keyboard.press('Control+Home');
  await page.keyboard.press('Control+Shift+End');
  await expect.poll(() => page.evaluate(() => window.getSelection()?.toString())).toBe('Read more');
  await expect(page.locator('[data-richtext-bubble]')).toBeVisible();
  await page.locator('[data-richtext-bubble] [data-richtext-command="link"]').click();
  await page.locator('[data-richtext-link-input]').fill('/more');
  await page.locator('[data-richtext-link-input]').press('Enter');
  await expect(page.locator('[data-richtext-link]')).toBeHidden();
  await expect(page.locator('[data-richtext] textarea')).toHaveValue('[Read more](/more)');
  const invalid = await page.locator('[data-richtext] textarea').evaluate((textarea: HTMLTextAreaElement) =>
    [...(textarea.form?.querySelectorAll(':invalid') ?? [])].map(control => ({
      id: control.id, type: (control as HTMLInputElement).type, value: (control as HTMLInputElement).value,
    })));
  console.log(`invalid controls before record save: ${JSON.stringify(invalid)}`);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  console.log(`record submissions after Save: ${submitted}`);
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').at(-1);
  const read = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(read.status()).toBe(200);
  expect((await read.json()).body).toContain('[Read more](/more)');
});
