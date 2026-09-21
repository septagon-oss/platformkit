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
const tasks = '/app/task/tasks';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('the section navigation is reachable below the sidebar breakpoint', async ({ page }) => {
  test.fail(); // Not fixed: the admin sidebar is `display: none` below lg and nothing discloses it.

  const visible = (locator: import('@playwright/test').Locator) =>
    locator.evaluateAll((nodes) =>
      nodes.filter((el) => {
        const box = el.getBoundingClientRect();
        return box.width > 0 && box.height > 0;
      }).length);

  // Precondition first, because a test that can only fail can also fail for the wrong
  // reason forever. If the selector stopped matching anything, this file would keep
  // reporting a defect after the defect was fixed, and the ratchet would never fire. At
  // desktop width the same locator must see section links; if it does not, the broken
  // thing here is the test.
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/app');
  const sectionLinks = page.locator('a[data-nav]');
  expect(await visible(sectionLinks), 'no a[data-nav] link is visible at 1280px: this test is broken, not the shell').toBeGreaterThan(0);

  // The honest question is not "is some <nav> visible" — a pagination nav has three links
  // and would answer it yes. It is whether a person on a phone can move to another part of
  // the application at all: either a section link they can see, or a control that discloses
  // the links.
  await page.setViewportSize({ width: 390, height: 844 });
  const links = await visible(sectionLinks);
  const disclosure = await visible(
    page.locator('button, summary, a[href]').filter({ hasText: /^(Menu|Navigation|Sections)$/i }),
  );
  expect(links + disclosure, 'no visible section link and no control that discloses one at 390px').toBeGreaterThan(0);
});

// The other half of the scroll-region work, and the half the markup cannot deliver.
//
// e2e/design-audit.spec.ts refuses a scroll box that is not in the tab order at all. That
// is a necessary condition and was briefly mistaken for a sufficient one, until it was
// measured: a bare `tabindex=0` `overflow:auto` div, in an otherwise empty document with no
// framework, no htmx and no CSP, does not move its scrollLeft on ArrowRight, End or Space
// under Chromium. Focus is not the same thing as scrolling, and a check that asserts the
// first while claiming the second is a proxy that flatters the change.
//
// Closing this is a decision, not a patch: a small scroll-keys behaviour in ui/assets/js
// (whose budget has 144 lines left), or a generated list that stops overflowing a phone
// width at all. Until somebody chooses, the claim is asserted here and allowed to fail.
test('a focused scroll region can be scrolled with the keyboard', async ({ page }) => {
  test.fail(); // Not fixed: focusable is not scrollable, not in this browser, not without behaviour.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(tasks);

  const region = page.locator('[data-component="table"]');
  await expect(region, 'the precondition: the region is in the tab order').toHaveAttribute('tabindex', '0');

  const hidden = await region.evaluate((el) => (el as HTMLElement).scrollWidth - el.clientWidth);
  expect(hidden, 'nothing overflows at this width, so this test would prove nothing').toBeGreaterThan(24);

  await region.focus();
  expect(await region.evaluate((el) => (el as HTMLElement).scrollLeft)).toBe(0);
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  const scrolled = await region.evaluate((el) => (el as HTMLElement).scrollLeft);
  expect(scrolled, `the columns past the fold (${hidden}px of them) are still unreachable without a pointer`).toBeGreaterThan(0);
});
