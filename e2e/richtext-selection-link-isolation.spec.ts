import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('a pasted address leaves the existing link in the stale selection untouched', async ({ page }) => {
  await openRichTextForm(page);
  const surface = page.locator(editingSurface);
  const selected = await surface.evaluate((target: HTMLElement & { editor?: any }) => {
    const editor = target.editor;
    editor.commands.setContent('<p><a href="https://example.com/existing">Existing link</a> Read <em>the manual</em> today</p>');
    editor.commands.setTextSelection({ from: 1, to: 14 });
    const paragraph = target.querySelector('p')!;
    const first = paragraph.childNodes[1];
    const last = paragraph.querySelector('em')!.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(last, 10, first, 1);
    const before = {
      words: selection.toString(),
      editor: { from: editor.state.selection.from, to: editor.state.selection.to },
    };
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com/manual');
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    return before;
  });
  expect(selected).toEqual({ words: 'Read the manual', editor: { from: 1, to: 14 } });
  await expect(surface.locator('a[href="https://example.com/existing"]')).toHaveText('Existing link');
  await expect.poll(async () => (await surface.locator('a[href="https://example.com/manual"]').allTextContents()).join('')).toBe('Read the manual');
  await expect(surface.locator('em')).toHaveText('the manual');
  await expect(surface).toHaveText('Existing link Read the manual today');
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/\[Existing link\]\(https:\/\/example\.com\/existing\)/);
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/https:\/\/example\.com\/manual/);
  await page.keyboard.type('Guide');
  await expect(surface).toHaveText('Existing link Guide today');
  await expect(surface.locator('a[href="https://example.com/existing"]')).toHaveText('Existing link');
});
