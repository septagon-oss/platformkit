import { expect, test } from '@playwright/test';

// Drive the served, sandboxed Gallery: no copied component HTML or controller.
for (const language of ['en', 'pt-PT']) {
  test(`photo-gallery keyboard navigation and focus (${language})`, async ({ page }) => {
    test.setTimeout(60_000);
    const signedIn = await page.request.post('/api/v1/auth/login', {
      data: { email: process.env.PLATFORMKIT_E2E_EMAIL, password: process.env.PLATFORMKIT_E2E_PASSWORD },
    });
    expect(signedIn.status()).toBe(200);

    for (const theme of ['light', 'dark']) for (const width of [360, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      const url = '/app/admin/_gallery/preview?' + new URLSearchParams({
        example: `pk-ui.component.photo-gallery/default-${language}`, theme,
      });
      expect((await page.goto(url))?.status()).toBe(200);
      const gallery = page.locator('[data-component="photo-gallery"]');
      await expect(gallery).toHaveAttribute('lang', language);
      const opener = gallery.locator('[data-photo-open]').first();
      await expect(opener).toBeVisible();
      await expect(opener).toHaveAttribute('href', '/images/one');
      await expect(opener).toHaveAccessibleName(/\S/);
      const firstPosition = await opener.getAttribute('aria-label');
      const lastPosition = await gallery.locator('[data-photo-open]').last().getAttribute('aria-label');

      await opener.focus();
      await page.keyboard.press('Enter');
      const viewer = page.getByRole('dialog');
      await expect(viewer).toBeVisible();
      await expect(viewer).toHaveAccessibleName(language === 'en' ? 'Items' : 'Itens');
      await expect(viewer.getByRole('status')).toHaveText(firstPosition!);
      const previous = viewer.locator('[data-photo-direction="-1"]');
      const next = viewer.locator('[data-photo-direction="1"]');
      await expect(previous).toBeDisabled();
      await expect(next).toBeEnabled();
      await page.keyboard.press('End');
      await expect(next).toBeDisabled();
      await expect(previous).toBeEnabled();
      await expect(viewer.getByRole('status')).toHaveText(lastPosition!);
      await page.keyboard.press('Home');
      await expect(previous).toBeDisabled();
      await expect(next).toBeEnabled();
      await expect(viewer.getByRole('status')).toHaveText(firstPosition!);

      const closeLink = viewer.locator('[data-photo-close]');
      await closeLink.focus();
      await page.keyboard.press('Tab');
      await expect(viewer.getByRole('button', { name: language === 'en' ? 'Close' : 'Fechar', exact: true })).toBeFocused();
      await page.keyboard.press('Shift+Tab');
      await expect(closeLink).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(viewer).toBeHidden();
      await expect(opener).toBeFocused();

      await page.keyboard.press('Enter');
      await expect(viewer).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(viewer).toBeHidden();
      await expect(opener).toBeFocused();
    }
  });
}
