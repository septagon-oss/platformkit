import { expect, test, type Page } from '@playwright/test';
import { FLOOR, expectFloor, probe, report } from './design_floor';
import { signInForContent } from './steps/content';

// The operator's home and the tenant list, measured by the floor's own instrument in both
// languages the bootstrapped tenant speaks. The tenant list met the whole floor before its
// copy changed, so it is held to the whole floor; the home is held to the measure, the one
// rule its new heading block brought.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

async function open(page: Page, path: string, language: string, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const response = await page.goto(`${path}?lang=${language}`);
  expect(response?.status(), `${path} in ${language}`).toBe(200);
  await expect(page.locator('main h1')).toHaveCount(1);
  await page.evaluate(() => document.fonts.ready);
}

test.beforeEach(async ({ page }) => {
  await signInForContent(page, admin.email, admin.password);
});

test('the workspace home keeps every paragraph within the reading measure in English and Portuguese', async ({ page }) => {
  for (const language of ['en', 'pt-PT']) {
    await open(page, '/app', language, 1440);
    const m = await probe(page, 1440);
    console.log(report(`home ${language}`, m));
    expect.soft(m.measures_ch.max, `home in ${language}: ${JSON.stringify(m.widest)}`).toBeLessThanOrEqual(FLOOR.maxMeasureCh);
  }
});

test('the tenant list holds the design floor at phone and desktop widths', async ({ page }) => {
  for (const width of [390, 1440]) {
    await open(page, '/app/tenant/tenants', 'en', width);
    await expectFloor(page, 'tenants', width);
  }
});
