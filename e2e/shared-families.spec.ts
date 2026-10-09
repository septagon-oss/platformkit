import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const root = resolve(__dirname, '..');
const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], { cwd: root, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }));
const scripts = readFileSync(resolve(root, 'ui/ui.go'), 'utf8').match(/var Controllers = \[\]string\{([\s\S]*?)\}/)![1].match(/"[^"]+\.js"/g)!.map(name => `<script defer src="/app/admin/assets/js/${JSON.parse(name)}"></script>`).join('');
const families = ['stepper', 'date-strip', 'slot-picker', 'calendar', 'product-card', 'option-chips', 'quantity-input', 'buy-bar', 'cart', 'order-summary', 'pricing-tiers', 'plan-comparison', 'map-view', 'photo-gallery', 'masonry', 'sparkline', 'area-chart', 'bar-chart', 'stat-tile'];
function html(id: string) { const example = snapshot.examples.find((e: { id: string }) => e.id === `pk-ui.component.${id}`); if (!example) throw new Error(id); return example.html; }
async function specimen(page: Page, id: string, theme = 'light', locale = 'en') {
  await page.route('**/__shared_families', route => route.fulfill({ contentType: 'text/html', body: `<!doctype html><html lang="${locale}" data-theme="${theme}"><head><meta charset="utf-8"><title>Shared components</title><style>${snapshot.css}</style>${scripts}</head><body><main>${html(id)}</main></body></html>` }));
  await page.goto('/__shared_families');
  await page.evaluate(() => document.fonts.ready);
}
for (const locale of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) test(`shared families: ${locale} / ${theme} accessible defaults and narrow reflow`, async ({ page }) => {
  test.setTimeout(180_000);
  for (const family of families) {
    await specimen(page, `${family}/default-${locale}`, theme, locale);
    const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
    expect(audit.violations, `${family} ${theme}`).toEqual([]);
    for (const width of [320, 390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      const geometry = await page.evaluate(() => ({ overflow: document.documentElement.scrollWidth > innerWidth, small: [...document.querySelectorAll('a[href],button,summary')].filter(el => { const b = el.getBoundingClientRect(); return b.width > 0 && b.height > 0 && (b.width < 43.9 || b.height < 43.9); }).map(el => el.textContent?.trim()) }));
      expect(geometry, `${family} at ${width}`).toEqual({ overflow: false, small: [] });
    }
  }
});
test('shared families: quantity preserves the lattice and exact int64 with one change event', async ({ page }) => {
  await specimen(page, 'quantity-input/default-en');
  const input = page.getByLabel('Quantity');
  await page.evaluate(() => { document.body.dataset.changes = '0'; document.addEventListener('change', () => { document.body.dataset.changes = String(Number(document.body.dataset.changes) + 1); }); });
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(input).toHaveValue('4'); await expect(page.locator('body')).toHaveAttribute('data-changes', '1');
  await input.fill('3'); await expect(page.getByRole('button', { name: 'Next', exact: true })).toBeDisabled(); expect(await input.evaluate((el: HTMLInputElement) => el.checkValidity())).toBe(false);
  await input.fill('8'); await expect(page.getByRole('button', { name: 'Next', exact: true })).toBeDisabled(); expect(await input.evaluate((el: HTMLInputElement) => el.checkValidity())).toBe(true);
  await specimen(page, 'quantity-input/overflow-boundary-en'); await page.getByRole('button', { name: 'Next', exact: true }).click(); await expect(page.getByLabel('Quantity')).toHaveValue('9007199254740994');
});
test('shared families: photo navigation ends and Escape restores thumbnail focus', async ({ page }) => {
  await specimen(page, 'photo-gallery/default-en');
  const opener = page.locator('[data-photo-open]').first(); await opener.click();
  const dialog = page.getByRole('dialog'); await expect(dialog).toBeVisible();
  await dialog.press('End'); await expect(dialog.getByRole('button', { name: 'Next', exact: true })).toBeDisabled();
  await dialog.press('Home'); await expect(dialog.getByRole('button', { name: 'Previous', exact: true })).toBeDisabled();
  await dialog.press('Escape'); await expect(dialog).toBeHidden(); await expect(opener).toBeFocused();
  await specimen(page, 'photo-gallery/removed-selection-en'); await expect(page.getByRole('dialog')).toBeHidden();
});
test('shared families: calendar engine uses supplied zone while agenda preserves repeated local time', async ({ page }) => {
  await specimen(page, 'calendar/day-en');
  await expect(page.locator('[data-calendar-engine]')).toHaveAttribute('data-engine-ready', 'true');
  await expect(page.locator('[data-calendar-event]')).toHaveCount(2);
  await expect(page.locator('[data-calendar-event="early"]')).toContainText('01:30 UTC+01:00');
  await expect(page.locator('[data-calendar-event="late"]')).toContainText('01:30 UTC+00:00');
  await expect(page.locator('[data-calendar-engine] a[href^="/events/"]')).toHaveCount(2);
  await page.locator('[data-component="calendar"]').evaluate((el, markup) => { document.dispatchEvent(new CustomEvent('htmx:beforeCleanupElement', { detail: { elt: el } })); el.outerHTML = markup; document.dispatchEvent(new CustomEvent('htmx:afterSwap')); }, html('calendar/week-en'));
  await expect(page.locator('[data-calendar-engine]')).toHaveAttribute('data-engine-ready', 'true'); await expect(page.locator('[data-calendar-engine] a[href^="/events/"]')).toHaveCount(2);
});
test('shared families: map pins retain native links and failed tiles retain the list', async ({ page }) => {
  await page.route('**/example-tiles/**', route => route.fulfill({ status: 404, body: '' }));
  await specimen(page, 'map-view/map-en');
  await expect(page.locator('[data-map-engine]')).toHaveAttribute('data-engine-ready', 'true');
  await expect(page.locator('.pk-map-marker')).toHaveCount(2); await expect(page.locator('.pk-map-marker').first()).toHaveAttribute('href', '/places/one');
  await expect(page.locator('[data-map-fallback]')).toBeVisible(); await expect(page.getByRole('table')).toBeVisible();
  await page.locator('[data-component="map-view"]').evaluate((el, markup) => { document.dispatchEvent(new CustomEvent('htmx:beforeCleanupElement', { detail: { elt: el } })); el.outerHTML = markup; document.dispatchEvent(new CustomEvent('htmx:afterSwap')); }, html('map-view/map-en'));
  await expect(page.locator('[data-map-engine]')).toHaveAttribute('data-engine-ready', 'true'); await expect(page.locator('.pk-map-marker')).toHaveCount(2);
});
test('shared families: native controls and POST forms work without enhancement', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false }); const page = await context.newPage();
  await specimen(page, 'stepper/default-en'); await expect(page.locator('form')).toHaveAttribute('method', 'post');
  expect(await page.getByRole('button', { name: 'Continue', exact: true }).getAttribute('form')).toBe('flow-en');
  await specimen(page, 'quantity-input/default-en'); await expect(page.getByLabel('Quantity')).toBeVisible(); await expect(page.getByRole('button', { name: 'Next', exact: true })).toBeHidden();
  await specimen(page, 'photo-gallery/default-en'); await expect(page.locator('[data-photo-open]').first()).toHaveAttribute('href', '/images/one');
  await specimen(page, 'plan-comparison/default-en'); await page.locator('summary').first().click(); await expect(page.locator('details').first()).toHaveAttribute('open', '');
  await context.close();
});
test('shared families: actual Gallery loads calendar assets under its sandbox policy', async ({ page }) => {
  await page.goto('/app/admin/login'); await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!); await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await page.waitForURL(/\/app$/);
  await page.goto('/app/admin/_gallery/preview?example=pk-ui.component.calendar%2Fday-pt-PT&theme=dark');
  await expect(page.locator('[data-calendar-engine]')).toHaveAttribute('data-engine-ready', 'true'); await expect(page.locator('[data-calendar-engine] a[href^="/events/"]')).toHaveCount(2);
  const styles = await page.locator('[data-calendar-engine]').evaluate(el => ({ loaded: [...document.styleSheets].some(s => s.href?.endsWith('fullcalendar-7.1.0.css')), height: el.getBoundingClientRect().height }));
  expect(styles).toEqual({ loaded: true, height: 384 });
});
