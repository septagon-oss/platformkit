import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('pasting a URL onto a settled keyboard selection preserves the words and adds the link', async ({ page }) => {
  await openRichTextForm(page);
  await page.keyboard.type('Website');
  await expect(page.locator(editingSurface)).toHaveText('Website');
  await page.keyboard.press('Shift+Home');
  await expect.poll(() => page.evaluate(() => window.getSelection()?.toString())).toBe('Website');
  await expect(page.locator('[data-richtext-bubble]')).toBeVisible();
  await page.evaluate((selector) => {
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com');
    document.querySelector(selector)!.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
  }, editingSurface);
  await expect(page.locator(`${editingSurface} a[href="https://example.com"]`)).toHaveText('Website');
  await expect(page.locator('[data-richtext] textarea')).toHaveValue('[Website](https://example.com)');
});
