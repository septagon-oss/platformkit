import { expect, test } from '@playwright/test';

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

// The design floor's other three rules on the page this delivery created: body
// text in at most two sizes, every text at 4.5:1 against what is behind it, and
// paragraphs no wider than 75 characters. Measured the way the floor measures.
test('the tenant languages page keeps one body size, readable contrast and a readable measure', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
  await page.goto('/app/tenant/languages');
  await expect(page.locator('select[name="default"]')).toBeVisible();

  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    const m = await page.evaluate(() => {
      const px = (v: string) => Math.round(parseFloat(v) || 0);
      const vis = (e: Element) => {
        const r = e.getBoundingClientRect();
        const s = getComputedStyle(e);
        return r.width > 40 && r.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
      };
      const parse = (c: string) => {
        const m = c && c.match(/rgba?\(([^)]+)\)/);
        if (!m) return null;
        const [r, g, b, a = 1] = m[1].split(',').map(Number);
        return { r, g, b, a };
      };
      const lum = ({ r, g, b }: { r: number; g: number; b: number }) => {
        const f = (v: number) => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); };
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
      };
      const bgOf = (e: Element) => {
        for (let n: Element | null = e; n; n = n.parentElement) {
          const c = parse(getComputedStyle(n).backgroundColor);
          if (c && c.a > 0) return c;
        }
        return { r: 255, g: 255, b: 255, a: 1 };
      };
      const paragraphs = [...document.querySelectorAll('p')].filter(vis);
      const sizes = [...new Set(paragraphs.map(e => px(getComputedStyle(e).fontSize)))].sort((a, b) => a - b);
      const measure = Math.max(0, ...paragraphs.map(e =>
        Math.round(e.getBoundingClientRect().width / (px(getComputedStyle(e).fontSize) * 0.5))));
      let worst = { ratio: 99, text: '' };
      const text = [...document.querySelectorAll('h1,h2,h3,h4,p,li,a,button,label,span,td,th')]
        .filter(vis).filter(e => (e.textContent || '').trim().length > 1);
      for (const e of text) {
        const fg = parse(getComputedStyle(e).color);
        if (!fg || fg.a === 0) continue;
        const [x, y] = [lum(fg), lum(bgOf(e))];
        const ratio = (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
        if (ratio < worst.ratio) worst = { ratio: Math.round(ratio * 100) / 100, text: (e.textContent || '').trim().slice(0, 40) };
      }
      return { sizes, measure, worst };
    });
    expect.soft(m.sizes.length, `${width}px body sizes: ${m.sizes.join(', ')}`).toBeLessThanOrEqual(2);
    expect.soft(m.worst.ratio, `${width}px contrast on "${m.worst.text}"`).toBeGreaterThanOrEqual(4.5);
    expect.soft(m.measure, `${width}px body measure in characters`).toBeLessThanOrEqual(75);
  }
});
