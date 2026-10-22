import { expect, test } from '@playwright/test';
import { expectFloor } from './design_floor';
import { save, signIn } from './steps/kernel';

test('task empty list column headings remain visible in both languages and widths', async ({ page }) => {
  test.setTimeout(120_000);
  const tasks = '/app/task/tasks';
  await signIn(page, `${tasks}/new`);
  await page.getByLabel('Title').fill('Keep the empty headings visible');
  await save(page);
  await expect(page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);

  for (const language of ['en', 'pt-PT']) {
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`${tasks}?lang=${language}`);
      await expect(page.locator(`table a[href^="${tasks}/"]`).first()).toBeVisible();
      const headings = (await page.getByRole('columnheader').allTextContents())
        .map(text => text.trim()).filter(Boolean);
      expect(headings.length).toBeGreaterThan(0);

      await page.goto(`${tasks}?page=9999&lang=${language}`);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      await expect(page.getByRole('table')).toBeVisible();
      await expect(page.locator(`table a[href^="${tasks}/"]`)).toHaveCount(0);
      for (const name of headings) {
        const heading = page.getByRole('columnheader').filter({ hasText: name });
        await expect(heading).toBeVisible();
        await expect(heading).toHaveAccessibleName(/\S/);
      }
      await expectFloor(page, `task visible empty headings in ${language}`, width);
    }
  }
});
