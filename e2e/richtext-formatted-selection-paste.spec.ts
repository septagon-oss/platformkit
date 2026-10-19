import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('a pasted address follows a backward selection across formatting and preserves its neighbours', async ({ page }) => {
  await openRichTextForm(page);
  const surface = page.locator(editingSurface);
  await expect(page.getByRole('textbox', { name: 'Content', exact: true })).toBeFocused();
  await page.keyboard.type('Read the manual today');
  const selected = await surface.evaluate((target: HTMLElement & { editor?: any }) => {
    const editor = target.editor;
    editor.commands.setTextSelection({ from: 10, to: 16 });
    editor.commands.toggleBold();
    editor.commands.setTextSelection({ from: 17, to: 22 });
    const first = target.querySelector('p')!.firstChild!;
    const last = target.querySelector('strong')!.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(last, 6, first, 0);
    const before = {
      words: selection.toString(),
      backward: selection.anchorNode === last && selection.anchorOffset === 6,
      editor: { from: editor.state.selection.from, to: editor.state.selection.to },
    };
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com/manual');
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
    return before;
  });
  expect(selected).toEqual({ words: 'Read the manual', backward: true, editor: { from: 17, to: 22 } });
  await expect.poll(async () => (await surface.locator('a[href="https://example.com/manual"]').allTextContents()).join('')).toBe('Read the manual');
  await expect(surface.locator('strong')).toHaveText('manual');
  await expect(surface).toHaveText('Read the manual today');
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/https:\/\/example\.com\/manual/);
  await page.keyboard.type('Guide');
  await expect(surface).toHaveText('Guide today');
});
