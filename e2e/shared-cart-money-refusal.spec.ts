import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { expect, test } from '@playwright/test';

const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
  cwd: resolve(__dirname, '..'), encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
}));

for (const language of ['en', 'pt-PT']) {
  for (const theme of ['light', 'dark']) {
    test(`cart refuses inconsistent money and suppresses stale checkout (${language}, ${theme})`, async ({ page }) => {
      await page.goto('/app/admin/login');
      await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
      await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      await expect(page).toHaveURL(/\/app$/);

      const example = `pk-ui.component.cart/default-${language}`;
      const original = snapshot.examples.find((entry: { id: string }) => entry.id === example).props;
      const preview = (props = original) => '/app/admin/_gallery/preview?' + new URLSearchParams({
        example, theme, props: JSON.stringify(props),
      });
      const lines = page.locator('[data-cart-line]');
      const checkout = page.locator('a[href="/checkout"]');

      // Reach the actual renderer through the valid line keys, independently
      // of the refusal's wording or the absence this test will assert below.
      expect((await page.goto(preview()))?.status()).toBe(200);
      expect(original.lines.length).toBeGreaterThan(0);
      await expect(lines).toHaveCount(original.lines.length);
      await expect(checkout).toHaveCount(1);
      await expect(page.locator('[data-component="cart"]')).toHaveAttribute('lang', language);

      for (const state of ['stale', 'pending', 'sold-out']) {
        const props = structuredClone(original);
        if (state === 'stale') props.quoteState = 'stale';
        if (state === 'pending') props.pending = true;
        if (state === 'sold-out') props.lines[0].availability = 'sold-out';
        expect((await page.goto(preview(props)))?.status()).toBe(200);
        await expect(lines).toHaveCount(original.lines.length);
        await expect(checkout).toHaveCount(0);
      }

      for (const defect of ['inconsistent-total', 'mixed-currency', 'overflow']) {
        const props = structuredClone(original);
        if (defect === 'inconsistent-total') {
          props.summary.total.money.minor = String(BigInt(props.summary.total.money.minor) + 1n);
        }
        if (defect === 'mixed-currency') {
          props.lines[0].lineTotal.money.currency = props.lines[0].unitPrice.money.currency === 'EUR' ? 'USD' : 'EUR';
        }
        if (defect === 'overflow') {
          props.lines[0].unitPrice.money.minor = '9223372036854775807';
          props.lines[0].quantity = props.lines[0].quantityInput.value = '2';
        }
        const refused = await page.request.get(preview(props), { timeout: 5_000 });
        expect(refused.status(), defect).toBeGreaterThanOrEqual(400);
        const body = await refused.text();
        expect(body, defect).not.toContain('data-cart-line');
        expect(body, defect).not.toContain('href="/checkout"');
      }

      expect((await page.goto(preview()))?.status()).toBe(200);
      await expect(lines).toHaveCount(original.lines.length);
      await expect(checkout).toHaveCount(1);
    });
  }
}
