import { expect, test } from '@playwright/test';
import { detail } from './steps/kernel';

// A generated detail field is addressed by its visible name, including punctuation.
test('detail finds a field whose name contains parentheses', async ({ page }) => {
  await page.setContent('<dl><div data-detail-item><dt>Cost (USD)</dt><dd>125</dd></div></dl>');
  await expect(detail(page, 'Cost (USD)')).toHaveText('125', { timeout: 1000 });
});

test('detail does not interpret punctuation as a pattern for another field', async ({ page }) => {
  await page.setContent('<dl><div data-detail-item><dt>Version 1X0</dt><dd>wrong row</dd></div>' +
    '<div data-detail-item><dt>Version 1.0</dt><dd>right row</dd></div></dl>');
  await expect(detail(page, 'Version 1.0')).toHaveText('right row', { timeout: 1000 });
});
