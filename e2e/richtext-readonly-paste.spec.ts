import { expect, test } from '@playwright/test';
import { openRichTextForm } from './steps/richtext';

// The preview is a read-only view of the document, so linking words by pasting a
// URL over them is an edit it refuses: the document and the posted value stay as
// they were. Selection and paste share one task, as they do when a person presses
// Ctrl+V straight after selecting.
test('a URL pasted over words in the preview leaves the document unchanged', async ({ page }) => {
  await openRichTextForm(page);
  await page.keyboard.type('Visit Website today');
  const posted = page.locator('[data-richtext] textarea');
  await expect(posted).toHaveValue('Visit Website today');

  await page.getByRole('button', { name: 'More formatting' }).click();
  await page.getByRole('button', { name: 'Preview as published' }).click();
  const preview = '[data-richtext-surface] [contenteditable="false"]';
  await expect(page.locator(preview)).toHaveText('Visit Website today');

  await page.evaluate((selector) => {
    const surface = document.querySelector(selector)!;
    const text = surface.querySelector('p')!.firstChild!;
    window.getSelection()!.setBaseAndExtent(text, 6, text, 13);
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com');
    surface.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
  }, preview);

  await expect(page.locator(`${preview} a`)).toHaveCount(0);
  await expect(page.locator(preview)).toHaveText('Visit Website today');
  await expect(posted).toHaveValue('Visit Website today');
});
