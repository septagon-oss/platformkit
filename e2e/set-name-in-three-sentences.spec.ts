import { expect, test } from '@playwright/test';
import { signInForContent } from './steps/content';

// One rule, read on the screens that can actually show it: a set is named by one word in
// every sentence said about it — the heading over the list, the count beside it, the crumb
// that leads back from a record, and the message a list with nothing in it gives you. The
// rule lives in kit/entity/display and ui/resource renders all four sentences through it;
// what these two journeys add is the markup, at a phone's 390px and a desktop's 1440px, so
// a drift back to the doubled form ("Contents") in one sentence, or at one width, fails in
// the browser that shows it.
//
// Which screen is asked which sentence is decided by what an arriving tenant has.
// seed/starter/contents.yaml writes two pages through the content module in the same
// transaction that created the tenant, so /app/content/contents has rows from the first
// minute and can only be asked the sentences a filled list says: its heading, its count,
// and the trail of one of its rows. The empty words belong to a list nothing provisions,
// and the plans list is that list — a plan is priced by an operator, so neither a tenant's
// arrival nor any journey of this suite writes one. e2e/admin-tasks.spec.ts measures the
// box that message sits in at 390px; what is asked here is its words, and of the count
// that has to agree with them, at both widths.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const contents = '/app/content/contents';

for (const width of [390, 1440]) {
  test(`the content list says its own noun in its heading, its count and its trail (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signInForContent(page, email, password);

    expect((await page.goto(contents))?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Content', exact: true })).toBeVisible();
    // The count is asked for a number rather than for zero, because this tenant has pages:
    // the noun is the claim, and "contents" is the doubling these sentences refuse.
    await expect(page.getByText(/^[1-9][0-9]* content$/)).toBeVisible();

    // One of those rows, opened the way a person opens it, leads back under the same word.
    const row = await page.locator(`a[href^="${contents}/"]`).evaluateAll(links =>
      links.map(link => (link as HTMLAnchorElement).getAttribute('href') ?? '')
        .find(href => /^[0-9a-f-]{36}$/.test(href.split('/').pop() ?? '')));
    expect(row, 'the content list draws no row a person could open').toBeTruthy();
    await page.goto(row!);
    await expect(page.locator('nav[data-component="breadcrumb"]')
      .locator(`a[href="${contents}"]`)).toHaveText('Content');
  });

  test(`an empty list says its own noun in its heading, its count and its message (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signInForContent(page, email, password);

    expect((await page.goto('/app/billing/plans'))?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Plans', exact: true })).toBeVisible();
    await expect(page.getByText('0 plans', { exact: true })).toBeVisible();
    await expect(page.getByText('No plans yet.', { exact: true })).toBeVisible();
  });
}
