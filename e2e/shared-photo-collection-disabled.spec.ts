import { expect, test } from '@playwright/test';

// Drive the served, sandboxed Gallery with one prop changed through the typed
// Props patch the preview route already accepts: a disabled collection stays
// readable and offers nothing to follow, by pointer, by key or by enhancement.
for (const language of ['en', 'pt-PT']) {
  test(`disabled photo collections keep their labels and lose their destinations (${language})`, async ({ page }) => {
    test.setTimeout(60_000);
    const signedIn = await page.request.post('/api/v1/auth/login', {
      data: { email: process.env.PLATFORMKIT_E2E_EMAIL, password: process.env.PLATFORMKIT_E2E_PASSWORD },
    });
    expect(signedIn.status()).toBe(200);

    for (const component of ['photo-gallery', 'masonry']) {
      await page.setViewportSize({ width: 1440, height: 900 });
      const url = '/app/admin/_gallery/preview?' + new URLSearchParams({
        example: `pk-ui.component.${component}/default-${language}`,
        props: JSON.stringify({ disabled: true }),
        theme: 'light',
      });
      expect((await page.goto(url))?.status()).toBe(200);
      const gallery = page.locator(`[data-component="${component}"]`);
      await expect(gallery).toHaveAttribute('lang', language);
      const thumbs = gallery.locator('a[aria-label]');
      expect(await thumbs.count()).toBeGreaterThan(0);
      for (const thumb of await thumbs.all()) {
        await expect(thumb).toHaveAccessibleName(/\S/);
        expect(await thumb.getAttribute('href'), `${component} thumbnail still names a destination`).toBeNull();
      }
      await expect(gallery.locator('[data-photo-open]')).toHaveCount(0);

      const opener = thumbs.first();
      await opener.focus();
      await page.keyboard.press('Enter');
      await opener.click({ force: true });
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(gallery).toBeVisible();
    }
  });
}
