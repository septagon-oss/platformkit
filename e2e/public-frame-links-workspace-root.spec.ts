import { expect, test } from '@playwright/test';

// Decision 0020: the anonymous public frame enters the workspace at its root,
// the address apps/platformkit/modules.go pins, and offers nothing deeper into
// it. A page with no entry at all is the same failure, so assert one exists.
test('the anonymous public frame links only the workspace root', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  const entries = await page.locator('a[href^="/app"]').evaluateAll(links =>
    links.map(link => link.getAttribute('href')));
  expect(entries.length).toBeGreaterThan(0);
  expect([...new Set(entries)]).toEqual(['/app']);
});
