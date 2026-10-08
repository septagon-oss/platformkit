import { expect, test } from '@playwright/test';

for (const language of ['en', 'pt-PT']) for (const javaScriptEnabled of [false, true]) {
  test.describe(`slot-picker native availability (${language}, JavaScript ${javaScriptEnabled})`, () => {
    test.use({ javaScriptEnabled });

    test('the served Gallery preserves eligible choices and refuses stale or unavailable choices', async ({ page }) => {
      test.setTimeout(60_000);
      const signedIn = await page.request.post('/api/v1/auth/login', {
        data: { email: process.env.PLATFORMKIT_E2E_EMAIL, password: process.env.PLATFORMKIT_E2E_PASSWORD },
      });
      expect(signedIn.status()).toBe(200);

      for (const theme of ['light', 'dark']) {
        for (const state of ['selected', 'booked', 'full', 'unavailable', 'past', 'stale', 'confirmation-pending', 'invalid-selection']) {
          const url = '/app/admin/_gallery/preview?' + new URLSearchParams({
            example: `pk-ui.component.slot-picker/${state}-${language}`, theme,
          });
          expect((await page.goto(url))?.status(), `${state}: preview must be reachable`).toBe(200);
          const picker = page.locator('[data-component="slot-picker"]');
          await expect(picker).toHaveAttribute('lang', language);
          const early = picker.locator('input[type="radio"][value="early"]');
          const late = picker.locator('input[type="radio"][value="late"]');
          await expect(early).toBeVisible();
          await expect(late).toBeVisible();
          await expect(early).toHaveAccessibleName(/01:30 UTC\+01:00/);
          await expect(late).toHaveAccessibleName(/01:30 UTC\+00:00/);

          const allUnavailable = state === 'stale' || state === 'confirmation-pending';
          if (state === 'selected' || state === 'invalid-selection') {
            await expect(early).toBeEnabled();
          } else {
            await expect(early).toBeDisabled();
          }
          if (state === 'selected') {
            await expect(early).toBeChecked();
          } else {
            await expect(picker.locator('input:checked')).toHaveCount(0);
          }
          if (allUnavailable) {
            await expect(late).toBeDisabled();
          } else {
            await expect(late).toBeEnabled();
            await late.focus();
            await page.keyboard.press('Space');
            await expect(late).toBeChecked();
            await expect(early).not.toBeChecked();
          }

          if (state === 'invalid-selection') {
            await expect(early).toHaveAttribute('aria-invalid', 'true');
            const errorID = await early.getAttribute('aria-describedby');
            expect(errorID).toBeTruthy();
            await expect(page.locator(`[id="${errorID}"]`)).toHaveText(language === 'en' ? 'Refresh before continuing.' : 'Atualize antes de continuar.');
          }
        }
      }
    });
  });
}
