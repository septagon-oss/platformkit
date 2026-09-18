import { expect, test } from '@playwright/test';

// Known defects, recorded where the gate can see them.
//
// This file exists so that an unfixed finding cannot become folklore. Each test asserts
// what the shell *should* do, and declares itself expected to fail. Today the run stays
// green because these tests fail. The day somebody fixes one, it starts passing, Playwright
// reports it as an error — "test was expected to fail, but passed" — and the run goes red
// until this entry is deleted. A defect listed here therefore has a half-life: it either
// gets fixed or it gets argued out of existence, and it cannot quietly become normal.
//
// Nothing here belongs in design-audit.spec.ts. That file measures what the family claims
// to have achieved; this one measures what it has not, and the two must never be conflated
// by an `expect.soft` or a TODO comment.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/admin$/);
});

test('the section navigation is reachable below the sidebar breakpoint', async ({ page }) => {
  test.fail(); // Not fixed: the admin sidebar is `display: none` below lg and nothing discloses it.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/admin');

  // The honest question is not "is some <nav> visible" — a pagination nav has three links
  // and would answer it yes. It is whether a person on a phone can move to another part of
  // the application at all: either a section link they can see, or a control that discloses
  // the links.
  const shown = (locator: import('@playwright/test').Locator) => locator.evaluateAll((nodes) =>
    nodes.filter((el) => {
      const box = el.getBoundingClientRect();
      return box.width > 0 && box.height > 0;
    }).length);

  const links = await shown(page.locator('a[data-nav]'));
  const disclosure = await shown(
    page.locator('button, summary, a[href]').filter({ hasText: /^(Menu|Navigation|Sections)$/i }),
  );
  expect(links + disclosure, 'no visible section link and no control that discloses one at 390px').toBeGreaterThan(0);
});
