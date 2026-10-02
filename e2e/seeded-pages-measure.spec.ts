import { expect, test } from '@playwright/test';

// The starter seed gives every new tenant two public pages, home and about, that
// did not exist before it. The design floor refuses a body line longer than 75
// characters (gates/design_gate.py: `width / (font-size × 0.5)` over every visible
// <p>), and these pages are measured at the width a laptop is. The way in is the
// seeded page's own heading, so the case reaches the measure through what the seed
// wrote, not through any defect.

/** Every visible body line wider than the gate's 75ch. */
async function wideLines(page: import('@playwright/test').Page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('p')]
      .filter((e) => {
        const box = e.getBoundingClientRect();
        const s = getComputedStyle(e);
        return box.width > 40 && box.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
      })
      .map((e) => {
        const size = parseFloat(getComputedStyle(e).fontSize);
        return {
          text: (e.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 40),
          ch: Math.round(e.getBoundingClientRect().width / (size * 0.5)),
        };
      })
      .filter((m) => m.ch > 75),
  );
}

for (const [path, heading] of [
  ['/home', 'Home'],
  ['/about', 'About us'],
] as const) {
  test(`the seeded ${path} page keeps its sentences to a readable measure at desktop width`, async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await context.newPage();
    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: heading }).first()).toBeVisible();
    expect(await wideLines(page)).toEqual([]);
    await context.close();
  });
}
