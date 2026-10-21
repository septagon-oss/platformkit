import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { signInForContent } from './steps/content';

test.beforeEach(async ({ page }) => {
  await signInForContent(page, process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test', process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
});

test('a workspace keeps people, permissions and work reachable at desktop and phone widths', async ({ page }) => {
  await expect(page.locator('main a[href="/app/auth/roles"]')).toBeVisible();
  await expect(page.locator('main a[href="/app/task/tasks"]')).toBeVisible();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    if (width < 1024) await page.locator('[data-component="sidebar-disclosure"] summary').click();
    const nav = width < 1024 ? page.locator('[data-component="sidebar-disclosure"]') : page.locator('aside');
    await expect(nav.getByRole('link', { name: 'Roles', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width < 1024) await page.locator('[data-component="sidebar-disclosure"] summary').click();
  }
});

test('people use the same names across list, new form and record', async ({ page }) => {
  await page.goto('/app/user/users');
  await expect(page.getByRole('heading', { name: 'People', exact: true })).toBeVisible();
  await expect(page.getByRole('columnheader', { name: 'Created at', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'New person', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'New person', exact: true })).toBeVisible();
  await page.goto('/app/user/users');
  const me = await (await page.request.get('/api/v1/auth/me')).json();
  await page.locator(`main a[href="/app/user/users/${me.userId}"]`).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
});

test('role summaries work with a keyboard and leave unopened editors out of the focus order', async ({ page }) => {
  await page.goto('/app/auth/roles');
  const summary = page.locator('summary').filter({ hasText: /^member ·/ });
  const form = page.getByRole('form', { name: 'What member grants', includeHidden: true });
  await expect(form).toBeHidden();
  await summary.focus();
  await page.keyboard.press('Enter');
  await expect(form).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(form).toBeHidden();
});

test('workspace, people and role summaries pass axe in both themes', async ({ page }) => {
  for (const theme of ['light', 'dark']) {
    // Load the stored theme before paint, as a returning user's browser does.
    // Changing it during an axe scan measures interpolated transition colors.
    await page.evaluate(value => localStorage.setItem('platformkit-theme', value), theme);
    for (const path of ['/app', '/app/user/users', '/app/auth/roles']) {
      await page.goto(path);
      const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
      expect(audit.violations, `${theme} ${path}`).toEqual([]);
    }
  }
});

test('access journeys have one heading, one main landmark and usable narrow forms', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const path of ['/app/admin/login', '/app/admin/login/forgot', '/app/admin/register', '/app/auth/verify-email', '/app/auth/reset']) {
    await page.goto(path);
    await expect(page.getByRole('main')).toHaveCount(1);
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
    expect(audit.violations, path).toEqual([]);
  }
});
