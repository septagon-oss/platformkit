import { expect, test } from '@playwright/test';

for (const javaScriptEnabled of [true, false]) {
  test(`login starts without an error box, JavaScript ${javaScriptEnabled}`, async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled, viewport: { width: 320, height: 700 } });
    try {
      const page = await context.newPage();
      await page.goto(new URL('/admin/login', process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').href);
      const error = page.locator('[data-login-error]');
      await expect(error).toBeHidden();
      expect(await error.boundingBox()).toBeNull();
      await expect(page.getByRole('alert')).toHaveCount(0);
      await page.getByLabel('Email').focus();
      await page.keyboard.press('Tab');
      await expect(page.getByLabel('Password')).toBeFocused();
      await page.keyboard.press('Tab');
      await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeFocused();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    } finally { await context.close(); }
  });
}

test('login reveals a real failure and hides it while retrying', async ({ page }) => {
  await page.goto('/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
  await page.getByLabel('Password').fill('incorrect-password-for-this-test');
  const button = page.getByRole('button', { name: 'Sign in', exact: true });
  await button.click();
  const error = page.locator('[data-login-error]');
  await expect(error).toBeVisible();
  await expect(error).toHaveAttribute('role', 'alert');
  await expect(error).toContainText(/invalid|credentials/i);
  await expect(button).toBeEnabled();
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/v1/auth/login', async route => { await gate; await route.continue(); });
  try {
    await button.click();
    await expect(button).toBeDisabled();
    await expect(error).toBeHidden();
    expect(await error.boundingBox()).toBeNull();
  } finally { release(); }
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
});
