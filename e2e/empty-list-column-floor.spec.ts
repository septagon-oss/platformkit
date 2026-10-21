import { expect, test } from '@playwright/test';
import { expectFloor } from './design_floor';
import { save, signIn } from './steps/kernel';

test('task empty lists retain their readable column headings in both languages', async ({ page }) => {
  test.setTimeout(120_000);
  const tasks = '/app/task/tasks';
  await signIn(page, `${tasks}/new`);
  await page.getByLabel('Title').fill('Keep the empty list columns');
  await save(page);
  await expect(page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);

  for (const language of ['en', 'pt-PT']) {
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`${tasks}?lang=${language}`);
      await expect(page.locator(`table a[href^="${tasks}/"]`).first()).toBeVisible();
      const headings = await page.locator('table thead th').allTextContents();
      const named = headings.map(text => text.trim()).filter(Boolean);
      expect(named.length).toBeGreaterThan(0);

      await page.goto(`${tasks}?page=9999&lang=${language}`);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      await expect(page.locator('table')).toBeVisible();
      await expect(page.locator(`table a[href^="${tasks}/"]`)).toHaveCount(0);
      const empty = await page.locator('table thead th').allTextContents();
      expect(empty.map(text => text.trim()).filter(Boolean)).toEqual(named);
      await expectFloor(page, `task empty columns in ${language}`, width);
    }
  }
});
