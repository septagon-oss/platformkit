import { expect, test } from '@playwright/test';

// The shell's inherited wrapping must reach record text without collapsing table columns.
test('task headings wrap while table columns stay scrollable and form help stays bounded', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const title = 'Z'.repeat(200);
  await page.goto('/app/task/tasks/new');
  await page.getByLabel('Title').fill(title);
  await page.getByLabel('Priority').selectOption('high');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);
  const record = new URL(page.url()).pathname;

  for (const width of [320, 390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(record);
    await expect(page.locator('h1')).toHaveText(title);
    expect(await page.locator('h1').evaluate(el => getComputedStyle(el).overflowWrap)).toBe('anywhere');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);

    await page.goto('/app/task/tasks');
    const rowLink = page.locator(`table a[href="${record}"]`);
    await expect(rowLink).toHaveText(title);
    expect(await rowLink.evaluate(el => getComputedStyle(el).overflowWrap)).toBe('normal');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    const region = page.locator('[data-component="table"]');
    await expect(region).toHaveAttribute('tabindex', '0');
    await region.focus();
    await page.keyboard.press('ArrowRight');
    await expect.poll(() => region.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
  }

  await page.goto(`${record}/edit`);
  const help = page.locator('main p[id$="-help"]');
  expect(await help.count()).toBeGreaterThan(0);
  for (const paragraph of await help.all()) {
    const measure = await paragraph.evaluate(el => el.getBoundingClientRect().width / (parseFloat(getComputedStyle(el).fontSize) * 0.5));
    expect(measure).toBeLessThanOrEqual(75);
  }
});
