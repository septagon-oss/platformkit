import { expect, test, type Page } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

// The editor reconciles its selection with the page on a timer that runs a moment
// after the surface takes focus. Whether that timer lands before or after the page
// reports a range the keyboard made a moment ago is a race no ordinary journey can
// choose to lose, and losing it once is enough to destroy the selection: the timer
// writes the editor's own copy of the caret over the person's range, and nothing
// downstream can recover words that are no longer highlighted.
//
// This file therefore decides the order itself. It keeps the timer's callback as the
// editor schedules it and calls that callback inside the same task that makes the
// range, which is the one order in which the callback cannot see the range: the page
// reports a new selection only after the current task ends. The callback is picked out
// by what it does — it asks the page for its selection range — rather than by its
// delay, because the editor schedules other timers of the same length for work that is
// not this reconciliation.
//
// What has to survive is the person's range, and the paste one step later proves it:
// the selected words keep their text and become a link.

// captureFocusReconciliation records, from the first script of the page on, every
// editor timer that reads the page's selection range. The list is what the test runs
// by hand; the timers themselves still run on their own.
async function captureFocusReconciliation(page: Page) {
  await page.addInitScript(() => {
    const captured: Array<() => void> = [];
    (window as any).__focusReconciliations = captured;
    const schedule = window.setTimeout.bind(window);
    (window as any).setTimeout = (callback: unknown, ms?: number, ...rest: unknown[]) => {
      if (ms === 20 && typeof callback === 'function' && String(callback).includes('domSelectionRange')) {
        captured.push(callback as () => void);
      }
      return schedule(callback as (...args: unknown[]) => void, ms, ...rest);
    };
  });
}

// raceTheFocusTimer draws the range Shift+Home leaves from a caret at the end of the
// words — the anchor stays where the caret was and the focus moves to their first letter,
// so the page holds a range that reads backwards exactly as the keyboard made it — and
// then runs the kept reconciliation callbacks before returning to the browser.
async function raceTheFocusTimer(page: Page) {
  return page.evaluate((selector) => {
    const words = document.querySelector(selector)!.querySelector('p')!.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(words, words.textContent!.length, words, 0);
    const selected = selection.toString();
    const reconciliations = (window as any).__focusReconciliations as Array<() => void>;
    for (const reconcile of reconciliations) reconcile();
    const after = window.getSelection()!;
    return {
      reconciled: reconciliations.length,
      selected,
      highlighted: !after.isCollapsed ? after.toString() : '',
    };
  }, editingSurface);
}

test('a focus timer that runs before the page reports a selection leaves that selection alone', async ({ page }) => {
  await captureFocusReconciliation(page);
  await openRichTextForm(page);
  await page.keyboard.type('Website');
  const surface = page.locator(editingSurface);
  await expect(surface).toHaveText('Website');

  const raced = await raceTheFocusTimer(page);
  expect(raced.reconciled, 'the editor must schedule a focus reconciliation for this to race').toBeGreaterThan(0);
  expect(raced.selected).toBe('Website');
  expect(raced.highlighted, 'the range the keyboard made must still be drawn on the page').toBe('Website');

  await page.evaluate((selector) => {
    const clipboard = new DataTransfer();
    clipboard.setData('text/plain', 'https://example.com');
    const target = document.querySelector(selector) as HTMLElement;
    target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
  }, editingSurface);

  await expect(surface.locator('a[href="https://example.com"]')).toHaveText('Website');
  await expect(page.locator('[data-richtext] textarea')).toHaveValue(/\[Website\]\(https:\/\/example\.com\)/);
});
