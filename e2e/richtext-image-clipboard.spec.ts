import { expect, test } from '@playwright/test';
import { editingSurface, imageBytes, openRichTextForm } from './steps/richtext';

test('an image pasted from the clipboard uploads once and enters the Markdown as a file reference', async ({ page }) => {
  await openRichTextForm(page);
  const requests: string[] = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().includes('/api/v1/file/files')) requests.push(request.url());
  });
  await page.evaluate(({ bytes, selector }) => {
    const clipboard = new DataTransfer();
    clipboard.items.add(new File([Uint8Array.from(bytes)], 'clipboard.png', { type: 'image/png' }));
    const target = document.querySelector(selector)!;
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
  }, { bytes: [...imageBytes], selector: editingSurface });
  await expect.poll(() => requests.length, { message: 'the clipboard image must reach the file upload API' }).toBe(1);
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/!\[.*\]\(pk-file:[0-9a-f-]{36}/);
});
