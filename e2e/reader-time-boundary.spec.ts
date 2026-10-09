import { expect, test } from '@playwright/test';
import { resolve } from 'node:path';

test.use({ timezoneId: 'Asia/Kolkata', locale: 'en-GB' });

test('a swapped instant crosses from relative words to a date exactly at seven days', async ({ page }) => {
  const now = new Date('2026-07-15T12:00:00Z');
  const week = 7 * 86_400_000;
  await page.clock.install({ time: now });
  await page.clock.pauseAt(now);
  await page.setContent('<html lang="en-GB"><body><main></main></body></html>');
  await page.addScriptTag({ path: resolve(__dirname, '../ui/assets/js/components.js') });

  // The same init used by a generated partial must discover newly inserted times.
  for (const age of [week - 1, week, -1]) {
    const instant = new Date(now.getTime() - age).toISOString();
    await page.evaluate((datetime) => {
      const el = document.createElement('time');
      el.dateTime = datetime;
      el.textContent = 'Server fallback';
      document.querySelector('main')!.replaceChildren(el);
      document.dispatchEvent(new CustomEvent('htmx:afterSwap', { detail: {} }));
    }, instant);
    const expected = await page.evaluate(({ instant, relative }) => {
      const at = new Date(instant);
      return {
        text: relative
          ? new Intl.RelativeTimeFormat('en-GB', { numeric: 'auto', style: 'long' }).format(-7, 'day')
          : new Intl.DateTimeFormat('en-GB', { dateStyle: 'medium', timeStyle: 'short' }).format(at),
        title: new Intl.DateTimeFormat('en-GB', { dateStyle: 'full', timeStyle: 'long' }).format(at),
      };
    }, { instant, relative: age >= 0 && age < week });
    await expect(page.locator('time')).toHaveText(expected.text);
    await expect(page.locator('time')).toHaveAttribute('title', expected.title);
    await expect(page.locator('time')).toHaveAttribute('datetime', instant);
  }
});
