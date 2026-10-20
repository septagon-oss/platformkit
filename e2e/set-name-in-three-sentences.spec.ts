import { expect, test } from '@playwright/test';
import { signInForContent } from './steps/content';

// Generated screens use the module's declared nouns in headings, counts and trails.
// The starter seed fills the content list with pages; billing plans remain empty
// and show the module's declared guidance. Check both states at phone and desktop widths.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const contents = '/app/content/contents';

for (const width of [390, 1440]) {
  test(`the content list says its own noun in its heading, its count and its trail (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signInForContent(page, email, password);

    expect((await page.goto(contents))?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Pages', exact: true })).toBeVisible();
    // The seed supplies pages; the count must use the same declared noun as the heading.
    await expect(page.getByText(/^[1-9][0-9]* pages$/)).toBeVisible();

    // One of those rows, opened the way a person opens it, leads back under the same word.
    const row = await page.locator(`a[href^="${contents}/"]`).evaluateAll(links =>
      links.map(link => (link as HTMLAnchorElement).getAttribute('href') ?? '')
        .find(href => /^[0-9a-f-]{36}$/.test(href.split('/').pop() ?? '')));
    expect(row, 'the content list draws no row a person could open').toBeTruthy();
    await page.goto(row!);
    await expect(page.locator('nav[data-component="breadcrumb"]')
      .locator(`a[href="${contents}"]`)).toHaveText('Pages');
  });

  test(`an empty list says its own noun in its heading, its count and its message (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signInForContent(page, email, password);

    expect((await page.goto('/app/billing/plans'))?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Plans', exact: true })).toBeVisible();
    await expect(page.getByText('0 plans', { exact: true })).toBeVisible();
    await expect(page.getByText('Create a plan with its price and billing interval.', { exact: true })).toBeVisible();
  });
}
