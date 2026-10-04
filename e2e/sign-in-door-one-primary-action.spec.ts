import { expect, test } from '@playwright/test';

// The sign-in page asks a person to do one thing per view: the design floor
// (gates/design_gate.py) refuses a page with more than one primary button above
// the fold, and the page passed it before the passkey button was added beside
// "Sign in". A second door is a secondary action — outlined, a link, anything but
// a second filled button of the primary colour. The count below is the floor's
// own: visible filled buttons and links, above the fold, sharing the most-used
// filled colour. Reached by the page rendering its own "Sign in" button, whatever else it draws.

for (const width of [390, 1440]) {
  test(`the sign-in page carries one primary action above the fold at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/app/admin/login', { waitUntil: 'networkidle' });
    await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
    const primary = await page.evaluate(() => {
      const visible = (e: Element) => {
        const r = e.getBoundingClientRect(); const s = getComputedStyle(e);
        return r.width > 40 && r.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
      };
      const filled = (e: Element) => {
        const m = getComputedStyle(e).backgroundColor.match(/rgba?\(([^)]+)\)/);
        if (!m) return false;
        const [r, g, b, a = 1] = m[1].split(',').map(Number);
        return a > 0 && r + g + b < 600;
      };
      const ctas = [...document.querySelectorAll('a,button')].filter(visible).filter(filled)
        .filter(e => e.getBoundingClientRect().top < innerHeight);
      const byColour: Record<string, number> = {};
      for (const e of ctas) { const k = getComputedStyle(e).backgroundColor; byColour[k] = (byColour[k] ?? 0) + 1; }
      return Math.max(0, ...Object.values(byColour));
    });
    expect(primary, `primary buttons above the fold at ${width}px`).toBeLessThanOrEqual(1);
  });
}
