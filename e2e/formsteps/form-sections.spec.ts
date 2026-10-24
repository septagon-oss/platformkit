import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

for (const javaScriptEnabled of [false, true]) {
  test(`three steps preserve answers and focus errors with scripting ${javaScriptEnabled}`, async ({ browser, baseURL }) => {
    const context = await browser.newContext({ baseURL, javaScriptEnabled, viewport: { width: 390, height: 844 } });
    await context.addCookies([{ name: 'session', value: 'present', url: baseURL! }]);
    const page = await context.newPage();
    const response = await page.goto('/app/note/notes/new');
    expect(response?.status(), await page.locator('body').innerText()).toBe(200);
    await expect(page.getByLabel('Title')).toBeVisible();
    if (javaScriptEnabled) expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
    await expect(page.getByLabel('Body', { exact: true })).toHaveCount(0);
    await expect(page.locator('[aria-current="step"]')).toHaveText('Details');
    const segments = page.locator('[data-step-key]');
    expect(await segments.nth(0).evaluate(el => getComputedStyle(el).borderTopColor)).not.toBe(await segments.nth(1).evaluate(el => getComputedStyle(el).borderTopColor));
    await page.getByLabel('Title').fill('reserved');
    if (javaScriptEnabled) await expect(page.getByRole('button', { name: 'Next · reserved' })).toBeVisible();
    await page.locator('button[value="next"]').click();
    expect(await segments.nth(0).evaluate(el => getComputedStyle(el).fontWeight)).not.toBe(await segments.nth(1).evaluate(el => getComputedStyle(el).fontWeight));
    await page.getByLabel('Body', { exact: true }).fill('draft');
    await page.getByLabel('Rank', { exact: true }).fill('7');
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Options', exact: true })).toBeVisible();
    await page.getByLabel('Tags', { exact: true }).fill('a, b');
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    await expect(page.getByLabel('Body', { exact: true })).toHaveValue('draft');
    await page.reload();
    await expect(page.getByLabel('Body', { exact: true })).toHaveValue('draft');
    // Submit a malformed numeric value through the actual native form. A browser
    // number control cannot type it, but a saved or modified form can send it.
    await page.getByLabel('Rank', { exact: true }).evaluate((input: HTMLInputElement) => { input.type = 'text'; input.value = 'bad-number'; });
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expect(page.getByLabel('Rank', { exact: true })).toHaveValue('bad-number');
    await expect(page.getByLabel('Rank', { exact: true })).toBeFocused();
    if (javaScriptEnabled) expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
    await page.getByLabel('Rank', { exact: true }).fill('7');
    await page.getByLabel('Body', { exact: true }).fill('edited');
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expect(page.getByLabel('Tags', { exact: true })).toHaveValue('a, b');
    await page.getByRole('button', { name: 'Save note', exact: true }).click();
    await expect(page.getByLabel('Title')).toHaveValue('reserved');
    await expect(page.getByLabel('Title')).toBeFocused();
    await expect(page.locator('[aria-current="step"]')).toHaveText('Details');
    await page.getByLabel('Title').fill('Café <b>notes</b>');
    if (javaScriptEnabled) {
      await expect(page.locator('button[value="next"]')).toHaveText('Next · Café <b>notes</b>');
      await expect(page.locator('button[value="next"] b')).toHaveCount(0);
    }
    await page.getByLabel('Title').press('Enter');
    await expect(page.getByLabel('Body', { exact: true })).toHaveValue('edited');
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await page.getByRole('button', { name: 'Save note', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Café <b>notes</b>', exact: true })).toBeVisible();
    await context.close();
  });
}
