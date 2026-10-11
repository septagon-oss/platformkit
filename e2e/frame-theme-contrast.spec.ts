import { expect, test } from '@playwright/test';
import { expectBrandLinkLegible, expectChromeOneStep, expectFooterBounded, probe } from './design_floor';
import { signInForContent } from './steps/content';

test('the admin frame keeps its legible brand and bounded chrome after a theme choice', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await signInForContent(page, process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
    process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  await page.goto('/app/task/tasks');

  const toggle = page.getByRole('button', { name: 'Switch between the light and dark theme' });
  const themes = new Set<string>();
  for (let choice = 0; choice < 2; choice++) {
    await toggle.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('html')).toHaveAttribute('data-theme', /^(light|dark)$/);
    const theme = await page.locator('html').getAttribute('data-theme');
    themes.add(theme!);
    // Reload observes the persisted choice after theme transitions have ended.
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme!);
    const measurement = await probe(page, 1440);
    console.log(`frame ${theme}: ${JSON.stringify(measurement.brand_link)}`);
    expectBrandLinkLegible(measurement, `the ${theme} admin brand link`);
    expectChromeOneStep(measurement, `the ${theme} frame`);
    expectFooterBounded(measurement, `the ${theme} footer`);
  }
  expect([...themes].sort()).toEqual(['dark', 'light']);
});
