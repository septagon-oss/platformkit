import { expect, test } from '@playwright/test';
import { resolve } from 'node:path';

test.use({ timezoneId: 'America/Los_Angeles', locale: 'en-GB' });

const now = new Date('2026-07-15T12:00:00Z');
const controller = resolve(__dirname, '../ui/assets/js/components.js');
const markup = (instant: string, kind = 'instant', relative = true, zone = '') =>
  `<time datetime="${instant}" data-time-policy="1" data-time-kind="${kind}" data-time-relative="${relative}" data-time-zone="${zone}" data-time-now="now">Server fallback</time>`;

test('resolved relative times truncate symmetrically before and after swaps', async ({ page }) => {
  await page.clock.install({ time: now });
  await page.clock.pauseAt(now);
  await page.setContent(`<html lang="en-GB"><body><main>${markup('2026-07-15T11:58:30Z')}</main></body></html>`);
  await page.addScriptTag({ path: controller });
  await expect(page.locator('time')).toHaveText('1 minute ago');
  const boundaries: [number, string, number][] = [
    [999, 'second', 0], [59_999, 'second', 59], [60_000, 'minute', 1],
    [3_599_999, 'minute', 59], [3_600_000, 'hour', 1], [86_399_999, 'hour', 23],
    [86_400_000, 'day', 1], [691_200_000, 'day', 8],
  ];
  for (const [distance, unit, number] of boundaries) {
    for (const direction of [-1, 1]) {
      const instant = new Date(now.getTime() + direction * distance).toISOString();
      await page.evaluate((html) => {
        document.querySelector('main')!.innerHTML = html;
        document.dispatchEvent(new CustomEvent('htmx:afterSwap', { detail: {} }));
      }, markup(instant));
      const count = number * direction;
      const text = number === 0 ? 'now' : direction < 0
        ? `${number} ${unit}${number === 1 ? '' : 's'} ago`
        : `in ${number} ${unit}${number === 1 ? '' : 's'}`;
      await expect(page.locator('time')).toHaveText(text);
      await expect(page.locator('time')).toHaveAttribute('data-time-unit', unit);
      await expect(page.locator('time')).toHaveAttribute('data-time-count', String(count));
      await expect(page.locator('time')).toHaveAttribute('datetime', instant);
    }
  }
});

test('resolved dates and absolute instants keep their policy across timezones and swaps', async ({ page }) => {
  await page.clock.install({ time: now });
  await page.setContent('<html lang="en-GB"><body><main></main></body></html>');
  await page.addScriptTag({ path: controller });
  const cases = [
    [markup('2026-07-01T00:30:00Z', 'date'), '2026-06-30'],
    [markup('2026-07-01T00:30:00Z', 'instant', false), '2026-06-30 17:30'],
    [markup('2026-07-01T00:30:00Z', 'date', true, 'UTC'), '2026-07-01'],
    [markup('2026-07-15T12:01:30Z', 'instant', false), '2026-07-15 05:01'],
    [markup('2026-07-15T11:58:30Z', 'instant', false), '2026-07-15 04:58'],
    [markup('2026-03-08T06:59:59Z', 'instant', false, 'America/New_York'), '2026-03-08 01:59'],
    [markup('2026-03-08T07:00:00Z', 'instant', false, 'America/New_York'), '2026-03-08 03:00'],
    [markup('2026-07-01T00:30:00Z', 'instant', false, 'Custom zone').replace('<time ', '<time data-time-offset="3600" '), '2026-07-01 01:30'],
    [markup('not-a-time'), 'Server fallback'],
  ];
  for (const [html, text] of cases) {
    await page.evaluate((html) => {
      document.querySelector('main')!.innerHTML = html;
      document.dispatchEvent(new CustomEvent('htmx:afterSwap', { detail: {} }));
    }, html);
    await expect(page.locator('time')).toHaveText(text);
  }
});

test('resolved time preserves server text when Intl is unavailable', async ({ page }) => {
  await page.setContent(`<html lang="en"><body>${markup('2026-07-15T12:01:30Z')}</body></html>`);
  await page.evaluate(() => Object.defineProperty(window, 'Intl', { value: undefined }));
  await page.addScriptTag({ path: controller });
  await expect(page.locator('time')).toHaveText('Server fallback');
});
