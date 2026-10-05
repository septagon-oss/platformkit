import { expect, test } from '@playwright/test';

for (const width of [390, 1440]) {
  for (const [path, heading] of [['/home', 'Home'], ['/about', 'About us']] as const) {
    test(`seeded ${path} has a consistent column and body scale at ${width}px`, async ({ browser }) => {
      const context = await browser.newContext({ viewport: { width, height: 900 } });
      try {
        const page = await context.newPage();
        const response = await page.goto(path);
        expect(response?.status()).toBe(200);
        await expect(page.getByRole('heading', { name: heading }).first()).toBeVisible();
        const measured = await page.evaluate(() => {
          const visible = (element: Element) => {
            const box = element.getBoundingClientRect();
            const style = getComputedStyle(element);
            return box.width > 40 && box.height > 6 && style.visibility !== 'hidden' && style.display !== 'none';
          };
          const lefts = [...new Set([...document.querySelectorAll('h1,h2,h3,p')]
            .filter(visible).map(element => Math.round(element.getBoundingClientRect().left)))].sort((a, b) => a - b);
          const columns: number[] = [];
          for (const left of lefts) {
            if (!columns.length || left - columns[columns.length - 1] > 3) columns.push(left);
          }
          const bodySizes = [...new Set([...document.querySelectorAll('p')].filter(visible)
            .map(element => Math.round(parseFloat(getComputedStyle(element).fontSize))))].sort((a, b) => a - b);
          return { columns, bodySizes };
        });
        expect.soft(measured.columns.length, `column edges ${JSON.stringify(measured.columns)}`).toBeLessThanOrEqual(2);
        expect.soft(measured.bodySizes.length, `body sizes ${JSON.stringify(measured.bodySizes)}`).toBeLessThanOrEqual(2);
      } finally {
        await context.close();
      }
    });
  }
}
