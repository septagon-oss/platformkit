import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('an address pasted before the editor reads a live range links only the selected words', async ({ page }) => {
  await openRichTextForm(page);
  await page.keyboard.type('Read the manual today');
  const surface = page.locator(editingSurface);
  await expect(surface).toHaveText('Read the manual today');
  const selected = await page.evaluate((selector) => {
    const target = document.querySelector(selector)!;
    const text = target.querySelector('p')!.firstChild!;
    const range = document.createRange();
    range.setStart(text, 9);
    range.setEnd(text, 15);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    const words = selection.toString();
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com/manual');
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    return words;
  }, editingSurface);
  expect(selected).toBe('manual');
  await expect(surface.locator('a[href="https://example.com/manual"]')).toHaveText('manual');
  await expect(surface).toHaveText('Read the manual today');
  await expect(page.locator('[data-richtext] textarea')).toHaveValue('Read the [manual](https://example.com/manual) today');
  await page.keyboard.type('guide');
  await expect(surface).toHaveText('Read the guide today');
});

test('a pasted address follows the live range after another range was selected', async ({ page }) => {
  await openRichTextForm(page);
  await page.keyboard.type('Read the manual today');
  const surface = page.locator(editingSurface);
  await page.evaluate((selector) => {
    const text = document.querySelector(selector)!.querySelector('p')!.firstChild!;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, 4);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
  }, editingSurface);
  await expect.poll(() => page.evaluate((selector) => {
    const target = document.querySelector(selector) as any;
    const view = target.editor?.view;
    return view ? { from: view.state.selection.from, to: view.state.selection.to } : null;
  }, editingSurface)).toEqual({ from: 1, to: 5 });
  await page.keyboard.press('Control+b');
  await expect(surface.locator('strong')).toHaveText('Read');
  const selected = await page.evaluate((selector) => {
    const target = document.querySelector(selector)!;
    const walker = document.createTreeWalker(target, NodeFilter.SHOW_TEXT);
    let text: Node | null;
    while ((text = walker.nextNode())) {
      const start = text.textContent!.indexOf('manual');
      if (start < 0) continue;
      const range = document.createRange();
      range.setStart(text, start);
      range.setEnd(text, start + 'manual'.length);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
      const words = selection.toString();
      const clipboard = new DataTransfer();
      clipboard.setData('text/plain', 'https://example.com/manual');
      target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
      return words;
    }
    return null;
  }, editingSurface);
  expect(selected).toBe('manual');
  await expect(surface.locator('a[href="https://example.com/manual"]')).toHaveText('manual');
  await expect(surface).toHaveText('Read the manual today');
});
