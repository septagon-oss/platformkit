import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.use({ locale: 'pt-PT' });

test('Portuguese sign-in keeps accessible labels and the existing keyboard authentication flow', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  const response = await page.goto('/admin/login');
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  expect(response?.headers()['content-language']).toBe('pt-PT');
  await expect(page.getByLabel('Email')).toBeFocused();
  await page.keyboard.type(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('Palavra-passe')).toBeFocused();
  await page.keyboard.type(process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: 'Iniciar sessão', exact: true })).toBeFocused();
  await expect(page.locator('[data-login-error]')).toHaveAttribute('lang', 'en');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).include('[data-login-form]').analyze()).violations).toEqual([]);
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
});

test('an English URL preference overrides the Portuguese browser without persisting it', async ({ page }) => {
  await page.goto('/admin/login?lang=en');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByLabel('Password')).toBeVisible();
  await page.goto('/admin/login');
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.getByLabel('Palavra-passe')).toBeVisible();
});
