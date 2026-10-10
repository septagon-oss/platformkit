import { expect, test } from '@playwright/test';
import { signInForContent } from './steps/content';

for (const width of [390, 1440]) {
  test(`content keeps the same noun in its heading, count and empty message at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signInForContent(page, process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
      process.env.PLATFORMKIT_E2E_PASSWORD ?? '');

    const response = await page.goto('/app/content/contents');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Content', exact: true })).toBeVisible();
    await expect(page.getByText('0 content', { exact: true })).toBeVisible();
    await expect(page.getByText('No content yet.', { exact: true })).toBeVisible();
  });
}
