import { expect, test } from '@playwright/test';

// A focused scroll region can be scrolled with the keyboard.
//
// This was e2e/known-defects.spec.ts's second entry: a `tabindex=0` `overflow:auto` region
// took focus but did not move on ArrowRight, measured once under Chromium. The browser now
// scrolls a focused scroller from the keyboard — measured 2026-09-29 through this repository's
// own e2e harness: five runs of two ArrowRight presses on the task list at 390px each settled
// at scrollLeft 80 — so the defect is gone without a line of ui/assets/js, and the file's rule
// is that a fixed defect leaves it. Its assertion lives here, expected to pass.
//
// The scroll is animated, and the entry it replaces read scrollLeft in the same tick as the
// key press: usually 0, which kept the defect "failing", and sometimes 2 or 3 pixels into the
// animation, which turned CI red as "expected to fail, but passed" (PR 55, run 145). So the
// read is polled until the region has moved.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('a focused scroll region can be scrolled with the keyboard', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/app/task/tasks');

  const region = page.locator('[data-component="table"]');
  await expect(region, 'the precondition: the region is in the tab order').toHaveAttribute('tabindex', '0');
  const hidden = await region.evaluate((el) => (el as HTMLElement).scrollWidth - el.clientWidth);
  expect(hidden, 'nothing overflows at this width, so this test would prove nothing').toBeGreaterThan(24);

  await region.focus();
  expect(await region.evaluate((el) => (el as HTMLElement).scrollLeft)).toBe(0);
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect
    .poll(() => region.evaluate((el) => (el as HTMLElement).scrollLeft), {
      message: `the columns past the fold (${hidden}px of them) are still unreachable without a pointer`,
    })
    .toBeGreaterThan(24);
});
