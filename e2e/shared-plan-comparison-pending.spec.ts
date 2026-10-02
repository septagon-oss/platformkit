import { expect, test } from '@playwright/test';

test.use({ reducedMotion: 'reduce' });

for (const language of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) {
  test(`pending plans retain keyboard disclosure and suppress purchase (${language}, ${theme})`, async ({ page }) => {
    const signedIn = await page.request.post('/api/v1/auth/login', {
      data: { email: process.env.PLATFORMKIT_E2E_EMAIL, password: process.env.PLATFORMKIT_E2E_PASSWORD },
    });
    expect(signedIn.status()).toBe(200);
    const url = '/app/admin/_gallery/preview?' + new URLSearchParams({
      example: `pk-ui.component.plan-comparison/pending-${language}`, theme,
    });
    expect((await page.goto(url))?.status()).toBe(200);
    await expect(page.locator('[data-component="plan-comparison"]')).toHaveAttribute('lang', language);

    // Native summaries reach the pending content without depending on its
    // English label, a redirect, or the presence of a forbidden purchase URL.
    const disclosure = page.locator('details').last();
    const summary = disclosure.locator('summary');
    await expect(summary).toBeVisible();
    await summary.focus();
    await page.keyboard.press('Enter');
    await expect(disclosure).toHaveAttribute('open', '');
    const pending = disclosure.locator('[data-loading="true"][data-component="button"]');
    await expect(pending).toBeVisible();
    await expect(pending).toHaveAttribute('aria-disabled', 'true');
    expect(await pending.getAttribute('href')).toBeNull();
    expect(await pending.getAttribute('tabindex')).toBe('-1');

    await page.addStyleTag({ content: '@keyframes review-plan { to { opacity: .5 } } summary { animation: review-plan 2s linear infinite !important; }' });
    const motion = await summary.evaluate(node => ({
      iterations: getComputedStyle(node).animationIterationCount,
      duration: Number.parseFloat(getComputedStyle(node).animationDuration),
    }));
    expect(motion).toEqual({ iterations: '1', duration: 0.00001 });
    await summary.focus();
    await page.keyboard.press('Enter');
    await expect(disclosure).not.toHaveAttribute('open');
    await expect(summary).toBeFocused();
    await expect(page).toHaveURL(new RegExp('/app/admin/_gallery/preview\\?'));
  });
}
