import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

const directory = mkdtempSync(join(tmpdir(), 'choice-fixtures-'));
execFileSync('go', ['test', './ui/forms', '-run', '^TestChoiceBrowserFixture$', '-count=1'], {
  cwd: resolve(__dirname, '..'), env: { ...process.env, PK_CHOICE_FIXTURES: directory }, timeout: 120_000,
});
test.afterAll(() => rmSync(directory, { recursive: true, force: true }));
test.use({ trace: 'off', screenshot: 'off', video: 'off' });

async function specimen(page: Page, appearance = 'tiles') {
  await page.route('http://choices.test/**', route => {
    const name = new URL(route.request().url()).pathname.slice(1);
    route.fulfill({ body: readFileSync(join(directory, name)), contentType: name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/html' });
  });
  await page.goto(`http://choices.test/${appearance}.html`);
}
async function selected(page: Page) {
  return page.locator('form').evaluate(form => new FormData(form as HTMLFormElement).getAll('shapes'));
}

test('arrows move focus, Space toggles and replacement preserves click order', async ({ page }) => {
  await specimen(page);
  const round = page.getByRole('checkbox', { name: 'Round', exact: true });
  const square = page.getByRole('checkbox', { name: 'Square', exact: true });
  await round.focus(); await page.keyboard.press('ArrowRight');
  await expect(square).toBeFocused(); expect(await selected(page)).toEqual(['round']);
  await page.keyboard.press('Space'); await expect(square).toBeChecked();
  await page.getByText('Straße', { exact: true }).click();
  await expect(round).not.toBeChecked();
  expect(await selected(page)).toEqual(['square', 'shape2']);
  await expect(page.locator('[data-choice-order]')).toHaveValue('["square","shape2"]');
  await expect(page.locator('[data-choice-count]')).toHaveText('2 selected');
  await expect(page.locator('[data-choice-stack] > *')).toHaveCount(2);
  await expect(page.locator('[data-choice-status]')).toContainText('Removed: Round');
});

test('refusing a maximum or last removal leaves the selection and announces why', async ({ page }) => {
  await specimen(page, 'chips');
  await page.getByText('Square', { exact: true }).click();
  expect(await selected(page)).toEqual(['round']);
  await expect(page.getByRole('status')).toHaveText('Choose at most one shape.');
  await page.getByRole('checkbox', { name: 'Round', exact: true }).press('Space');
  expect(await selected(page)).toEqual(['round']);
  await expect(page.getByRole('status')).toHaveText('Keep a shape.');
});

test('Unicode search retains checked values and No preference clears them', async ({ page }) => {
  await specimen(page, 'swatch');
  await page.getByRole('searchbox').fill('STRASSE');
  await expect(page.getByRole('checkbox', { name: 'Straße', exact: true })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Round', exact: true })).toBeHidden();
  expect(await selected(page)).toEqual(['round']);
  await page.getByRole('checkbox', { name: 'No preference', exact: true }).click();
  expect(await selected(page)).toEqual(['none']);
  await expect(page.locator('[data-choice-stack] > *')).toHaveCount(0);
  await page.getByRole('searchbox').fill('absent');
  await expect(page.getByRole('status')).toHaveText('No matching choices');
});

test('a figure and its native choices stay synchronized across reset and repeated mounts', async ({ page }) => {
  await specimen(page, 'figure');
  await expect(page.locator('[data-choice-figure] [data-choice=round][data-selected=true]')).toHaveCount(2);
  const region = page.locator('[data-choice-figure] [data-choice=square]');
  await region.click();
  await expect(page.getByRole('checkbox', { name: 'Square', exact: true })).toBeChecked();
  await expect(region).toHaveAttribute('data-selected', 'true');
  await page.locator('form').evaluate(form => (form as HTMLFormElement).reset());
  await expect(region).not.toHaveAttribute('data-selected', 'true');
  await page.evaluate(() => { document.dispatchEvent(new Event('htmx:afterSwap')); document.dispatchEvent(new Event('htmx:afterSwap')); });
  await region.click();
  expect(await selected(page)).toEqual(['round', 'square']);
  await page.locator('[data-choice-surface]').evaluate(node => { node.outerHTML = node.outerHTML; document.dispatchEvent(new Event('htmx:afterSwap')); });
  await page.locator('[data-choice-figure] [data-choice=square]').click();
  expect(await selected(page)).toEqual(['round', 'square']);
});

for (const appearance of ['tiles', 'chips', 'swatch', 'figure']) {
  test(`${appearance} remains a native form without JavaScript`, async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 800 } });
    try {
      const page = await context.newPage(); await specimen(page, appearance);
      await page.getByRole('checkbox', { name: 'Square', exact: true }).check();
      expect(await selected(page)).toEqual(['round', 'square']);
      await expect(page.locator('[data-choice-search-label]')).toBeHidden();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const label = page.locator('[data-choice-item=square]');
      expect((await label.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    } finally { await context.close(); }
  });
}


test('focus and the checked badge remain visible in both themes and forced colors', async ({ page }) => {
  await specimen(page, 'swatch');
  for (const forcedColors of ['none', 'active'] as const) {
    await page.emulateMedia({ forcedColors, reducedMotion: 'reduce' });
    for (const theme of ['light', 'dark']) {
      await page.locator('html').evaluate((node, theme) => node.setAttribute('data-theme', theme), theme);
      await page.getByRole('checkbox', { name: 'Round', exact: true }).focus();
      const label = page.locator('[data-choice-item=round]');
      await expect(label.locator('[data-choice-check]')).toBeVisible();
      expect(await label.evaluate(node => {
        const css = getComputedStyle(node);
        return css.outlineStyle !== 'none' && parseFloat(css.outlineWidth) >= 2;
      })).toBe(true);
    }
  }
});
