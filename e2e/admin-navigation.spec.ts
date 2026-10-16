import { expect, test } from '@playwright/test';

// The admin section navigation below the sidebar breakpoint.
//
// This was e2e/known-defects.spec.ts's first entry: the admin sidebar is `display: none`
// below lg and nothing disclosed the sections, so a person on a phone could not move to
// another part of the application. components.SidebarDisclosure fixed it — a native
// <details>/<summary> "Menu" in the header, listing the sidebar's own sections, hidden
// from lg up — and the file's rule is that a fixed defect leaves it. Its assertion lives
// here now, expected to pass.
//
// Every layout read after a viewport change is polled: the entry it replaces read the page
// in the same tick as `setViewportSize` and so, on a busy CI runner, sometimes still saw the
// desktop sidebar, passed, and turned the run red as "expected to fail, but passed".

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('the section navigation is reachable below the sidebar breakpoint', async ({ page }) => {
  const visible = (locator: import('@playwright/test').Locator) =>
    locator.evaluateAll((nodes) =>
      nodes.filter((el) => {
        const box = el.getBoundingClientRect();
        return box.width > 0 && box.height > 0;
      }).length);
  const sectionLinks = page.locator('a[data-nav]');
  const menu = page.locator('summary').filter({ hasText: /^Menu$/ });

  // Precondition: at desktop width the sidebar shows section links and the disclosure is
  // hidden, so a selector that stopped matching fails here rather than passing for nothing.
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/app');
  await expect.poll(() => visible(sectionLinks), { message: 'no section link at 1280px' }).toBeGreaterThan(0);
  await expect(menu).toBeHidden();

  // At phone width the sidebar is gone and the disclosure is the way to the sections.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(menu).toBeVisible();
  await menu.click();
  const disclosed = page.locator('[data-component="sidebar-disclosure"] a[data-nav]');
  await expect.poll(() => visible(disclosed), { message: 'the open disclosure shows no section link at 390px' })
    .toBeGreaterThan(0);

  // And a disclosed link goes where the sidebar's does.
  const first = disclosed.first();
  const href = await first.getAttribute('href');
  await first.click();
  await expect(page).toHaveURL(new RegExp(`${href?.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`));
});
