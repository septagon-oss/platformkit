import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.use({ locale: 'pt-PT' });

test('Portuguese sign-in keeps accessible labels and the existing keyboard authentication flow', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  const response = await page.goto('/app/admin/login');
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
  await expect(page).toHaveURL(/\/app$/);
  // The two assertions above are about a page nobody has signed in to. This one is
  // about the page they arrive at, and the branch this spec belongs to changed what
  // it says: a person who has chosen no language of their own is answered in the one
  // their browser asked for, because an account preference is a choice and not a
  // default, and the fallback chain is choice, then browser, then the tenant's
  // default. This deployment serves pt-PT and the browser here declares pt-PT, so a
  // Portuguese sign-in now ends on a Portuguese dashboard — which is the brief's own
  // acceptance line. Expecting English here would be expecting the bug: the page used
  // to declare pt-PT over English chrome and the fix was to make the copy follow the
  // declaration. `lang` and `dir` are both asserted, and the heading in the same
  // language, so a page that translated one and forgot the other fails here rather
  // than passing on a half-localised render.
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr');
  await expect(page.getByRole('heading', { name: 'Painel', exact: true })).toBeVisible();
});

test('an English URL preference overrides the Portuguese browser without persisting it', async ({ page }) => {
  await page.goto('/app/admin/login?lang=en');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByLabel('Password')).toBeVisible();
  await page.goto('/app/admin/login');
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.getByLabel('Palavra-passe')).toBeVisible();
});
