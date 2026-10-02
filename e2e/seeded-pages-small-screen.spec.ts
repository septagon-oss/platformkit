import { expect, test } from '@playwright/test';

for (const [path, heading] of [
  ['/home', 'Home'],
  ['/about', 'About us'],
] as const) {
  test(`the seeded ${path} page fits a small screen`, async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();
    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: heading }).first()).toBeVisible();

    const measure = await page.evaluate(() => ({
      overflow: document.documentElement.scrollWidth > window.innerWidth,
      wideParagraphs: [...document.querySelectorAll('p')]
        .filter((paragraph) => {
          const box = paragraph.getBoundingClientRect();
          const style = getComputedStyle(paragraph);
          return box.width > 40 && box.height > 6 && style.visibility !== 'hidden' && style.display !== 'none';
        })
        .filter((paragraph) => {
          const size = parseFloat(getComputedStyle(paragraph).fontSize);
          return paragraph.getBoundingClientRect().width / (size * 0.5) > 75;
        })
        .map((paragraph) => paragraph.textContent?.trim().slice(0, 40)),
    }));
    expect(measure).toEqual({ overflow: false, wideParagraphs: [] });
    await context.close();
  });
}
