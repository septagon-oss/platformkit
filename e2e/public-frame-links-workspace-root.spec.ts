import { expect, test } from '@playwright/test';

// Decision 0020: the public frame enters the workspace at its root. This
// pre-existing LOW mismatch is deferred in the round-6 review, not a HIGH hold
// against the shared components. Keep the correct behavior executable.
test('the anonymous public frame links only the workspace root', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  const entries = await page.locator('a[href^="/app"]').evaluateAll(links =>
    links.map(link => link.getAttribute('href')));
  expect(entries.length).toBeGreaterThan(0);
  expect([...new Set(entries)]).toEqual(['/app']);
});
