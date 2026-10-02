import { expect, test } from '@playwright/test';

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test('the tenant languages page keeps one content column at phone and desktop widths', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
  await page.goto('/app/tenant/languages');
  await expect(page.locator('select[name="default"]')).toBeVisible();

  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    const leftEdges = await page.evaluate(() => {
      const px = (value: number) => Math.round(value);
      const visible = (element: Element) => {
        const box = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        return box.width > 40 && box.height > 6 && style.visibility !== 'hidden' && style.display !== 'none';
      };
      const blocks = [...document.querySelectorAll('h1,h2,h3,p,section,article,main > *,header,footer')]
        .filter(visible).filter(element => /^(H[1-3]|P)$/.test(element.tagName));
      const lefts = [...new Set(blocks.map(element => px(element.getBoundingClientRect().left)))].sort((a, b) => a - b);
      const clusters: number[] = [];
      for (const left of lefts) {
        if (clusters.length === 0 || left - clusters[clusters.length - 1] > 3) clusters.push(left);
      }
      return clusters;
    });
    expect(leftEdges.length, `${width}px left edges: ${leftEdges.join(', ')}`).toBeLessThanOrEqual(2);
  }
});
