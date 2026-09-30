import { expect, test } from '@playwright/test';

for (const language of ['en', 'pt-PT']) {
  for (const kind of ['side-panel', 'detail-sheet']) {
    test(`review 4: ${kind} rejects retained fields before Gallery output (${language})`, async ({ page }) => {
      await page.goto('/app/admin/login');
      await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
      await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      await expect(page).toHaveURL(/\/app$/);

      const query = new URLSearchParams({
        example: `pk-ui.component.${kind}/default-${language}`,
        theme: 'dark',
      });
      const preview = () => '/app/admin/_gallery/preview?' + query;
      expect((await page.goto(preview()))?.status()).toBe(200);
      await expect(page.locator('[data-detail-item="a"]')).toHaveCount(1);
      await expect(page.locator('#detail-note')).toHaveCount(1);

      // Editing portable Props must also validate the already captured slots.
      // Reach the renderer through the valid item above, not an error sentence.
      query.set('props', JSON.stringify({
        itemID: '', title: '', description: '',
        state: {
          status: 'refused',
          title: language === 'en' ? 'Access' : 'Acesso',
          text: language === 'en' ? 'No access.' : 'Sem acesso.',
        },
      }));
      for (const path of [preview(), '/app/admin/_gallery?' + query]) {
        const response = await page.request.get(path, { timeout: 5_000 });
        expect(response.status()).toBeGreaterThanOrEqual(400);
        const body = await response.text();
        expect(body).not.toContain('detail-note');
        expect(body).not.toContain('data-detail-item');
      }

      query.delete('props');
      expect((await page.goto(preview()))?.status()).toBe(200);
      await expect(page.locator('#detail-note')).toHaveCount(1);
    });
  }
}
