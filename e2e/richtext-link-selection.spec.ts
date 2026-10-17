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

for (const backward of [false, true]) {
  test(`a URL pasted before the ${backward ? 'backward' : 'forward'} selection event links the selected words`, async ({ page }) => {
    await openRichTextForm(page);
    await page.keyboard.type('Visit Website today');
    await page.evaluate(({ selector, backward }) => {
      const surface = document.querySelector(selector)!;
      const text = surface.querySelector('p')!.firstChild!;
      // Selection and paste share a task, before selectionchange can update the editor.
      window.getSelection()!.setBaseAndExtent(text, backward ? 13 : 6, text, backward ? 6 : 13);
      const clipboard = new DataTransfer();
      clipboard.setData('text/plain', 'https://example.com');
      surface.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    }, { selector: editingSurface, backward });
    await expect(page.locator(`${editingSurface} a[href="https://example.com"]`)).toHaveText('Website');
    await expect(page.locator('[data-richtext] textarea')).toHaveValue('Visit [Website](https://example.com) today');
  });
}
