import { expect, test } from '@playwright/test';
import { securityHeaders, signIn } from './steps/kernel';

test('the public response satisfies the published header step', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  securityHeaders(response, 'public');
});

test('the workspace response satisfies the published header step', async ({ page }) => {
  await signIn(page);
  const response = await page.goto('/app');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  securityHeaders(response, 'desk');
});
