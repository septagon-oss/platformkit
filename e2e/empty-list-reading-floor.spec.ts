import { expect, test } from '@playwright/test';
import { expectFloor } from './design_floor';
import { save, signIn } from './steps/kernel';

// The task module declares the guidance; the kernel's generated list must keep
// those words at the count's body size in both browser and catalogue languages.
test('task empty guidance matches its catalogue and keeps the generated list body size', async ({ page }) => {
  test.setTimeout(120_000);
  const tasks = '/app/task/tasks';
  await signIn(page, `${tasks}/new`);
  await page.getByLabel('Title').fill('Keep the empty guidance readable');
  await save(page);
  await expect(page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);

  for (const language of ['en', 'pt-PT']) {
    const response = await page.request.get('/api/v1/app/resources', {
      headers: { 'Accept-Language': language },
    });
    expect(response.status()).toBe(200);
    const catalogue = await response.json();
    const task = catalogue.resources.find((entry: { module: string; entity: string }) =>
      entry.module === 'task' && entry.entity === 'task');
    const description = task?.presentation?.emptyDescription;
    expect(typeof description).toBe('string');
    expect(description.length).toBeGreaterThan(0);

    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`${tasks}?lang=${language}`);
      await expect(page.locator(`table a[href^="${tasks}/"]`).first()).toBeVisible();
      const countSize = await page.locator('[data-component="data-list"] > p').first()
        .evaluate(element => getComputedStyle(element).fontSize);

      await page.goto(`${tasks}?page=9999&lang=${language}`);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      await expect(page.locator('table')).toBeVisible();
      await expect(page.locator(`table a[href^="${tasks}/"]`)).toHaveCount(0);
      const guidance = page.locator('main').getByText(description, { exact: true });
      await expect(guidance).toHaveCount(1);
      await expect(guidance).toBeVisible();
      expect(await guidance.evaluate(element => element.tagName)).toBe('P');
      expect(await guidance.evaluate(element => getComputedStyle(element).fontSize)).toBe(countSize);
      await expectFloor(page, `task empty guidance in ${language}`, width);
    }
  }
});
