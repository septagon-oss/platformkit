import { expect, test } from '@playwright/test';
import { signIn } from './steps/kernel';

// Content and billing declare their words once for the catalogue and generated screens.
// Follow a seeded page and an empty plan list in each request language and viewport.
for (const language of ['en', 'pt-PT']) {
  for (const width of [390, 1440]) {
    test(`content and billing keep their catalogue words on the workspace (${language}, ${width}px)`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await signIn(page);
      const localized = (path: string) => `${path}?lang=${language}`;
      const response = await page.request.get(localized('/api/v1/app/resources'), {
        headers: { 'Accept-Language': language },
      });
      expect(response.status(), await response.text()).toBe(200);
      const { resources } = await response.json();
      const content = resources.find((entry: { module: string; entity: string }) =>
        entry.module === 'content' && entry.entity === 'content');
      const plans = resources.find((entry: { module: string; entity: string }) =>
        entry.module === 'billing' && entry.entity === 'plan');
      expect(content?.presentation?.plural).toBeTruthy();
      expect(plans?.presentation?.emptyDescription).toBeTruthy();

      expect((await page.goto(localized(content.screen)))?.status()).toBe(200);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      const heading = page.getByRole('heading', { level: 1 });
      await expect(heading).toHaveText(new RegExp(`^${content.presentation.plural}$`, 'iu'));
      await expect(page.getByText(new RegExp(`^[1-9][0-9]* ${content.presentation.plural}(?:\\s|$)`, 'u')))
        .toBeVisible();
      const row = page.locator(`table a[href^="${content.screen}/"]`).first();
      await expect(row).toBeVisible();
      const address = await row.getAttribute('href');
      expect(address).toBeTruthy();
      expect((await page.goto(localized(address!)))?.status()).toBe(200);
      await expect(page).toHaveURL(localized(address!));
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      await expect(page.locator('nav[data-component="breadcrumb"]')
        .locator(`a[href="${content.screen}"]`))
        .toHaveText(new RegExp(`^${content.presentation.plural}$`, 'iu'));

      expect((await page.goto(localized(plans.screen)))?.status()).toBe(200);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      await expect(page.getByRole('heading', { level: 1 }))
        .toHaveText(new RegExp(`^${plans.presentation.plural}$`, 'iu'));
      await expect(page.getByText(plans.presentation.emptyDescription, { exact: true })).toBeVisible();
    });
  }
}
