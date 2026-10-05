import { expect, test } from '@playwright/test';

// A disabled Calendar or MapView keeps what it shows and loses where it goes. The
// bar (ui/components/shared-web-components.md) is that a disabled navigation choice
// cannot be followed by mouse, keyboard or enhancement, and the agenda and the point
// list already meet it — the day grid and the marker layer are the parts that build
// their own anchors out of the payload the renderer hands the engine, so each case
// below reads the destination off the served page rather than trusting a flag.
for (const language of ['en', 'pt-PT']) {
  test.describe(`disabled enhanced views (${language})`, () => {
    test('the calendar grid and the map pins keep their content and offer no destination', async ({ page }) => {
      test.setTimeout(120_000);
      const signedIn = await page.request.post('/api/v1/auth/login', {
        data: { email: process.env.PLATFORMKIT_E2E_EMAIL, password: process.env.PLATFORMKIT_E2E_PASSWORD },
      });
      expect(signedIn.status()).toBe(200);
      const preview = (example: string, disabled: boolean) => '/app/admin/_gallery/preview?' + new URLSearchParams({
        example: `pk-ui.component.${example}-${language}`, theme: 'dark', props: JSON.stringify({ disabled }),
      });
      const section = page.locator('section[data-component="calendar"]');

      for (const view of ['day', 'week']) {
        await page.goto(preview(`calendar/${view}`, false));
        const engine = page.locator('[data-calendar-engine]');
        await expect(engine).toHaveAttribute('data-engine-ready', 'true');
        await expect(engine.locator('.fc-event')).toHaveCount(2);
        await expect(engine.locator('a[href^="/events/"]')).toHaveCount(2);
        await expect(section.locator('a[href^="/events/"]')).toHaveCount(4);

        await page.goto(preview(`calendar/${view}`, true));
        await expect(engine).toHaveAttribute('data-engine-ready', 'true');
        await expect(engine.locator('.fc-event')).toHaveCount(2);
        await expect(engine.locator('a[href]')).toHaveCount(0);
        await expect(section.locator('a[href^="/events/"]')).toHaveCount(0);
        // Nothing inside the disabled view holds the keyboard, so Enter answers it
        // with nothing. The address the enabled grid offered is never reached.
        await engine.focus();
        await page.keyboard.press('Enter');
        await expect(page).toHaveURL(/\/app\/admin\/_gallery\/preview\?/);
      }

      const pins = () => page.locator('.pk-map-marker').evaluateAll(els =>
        els.map(el => [el.tagName, el.getAttribute('href'), (el.getAttribute('aria-label') ?? '').length > 0]));
      await page.goto(preview('map-view/map', false));
      await expect(page.locator('[data-map-engine]')).toHaveAttribute('data-engine-ready', 'true');
      expect(await pins()).toEqual([['A', '/places/one', true], ['A', '/places/two', true]]);

      await page.goto(preview('map-view/map', true));
      await expect(page.locator('[data-map-engine]')).toHaveAttribute('data-engine-ready', 'true');
      expect(await pins()).toEqual([['SPAN', null, true], ['SPAN', null, true]]);
      await page.locator('.pk-map-marker').first().focus();
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/\/app\/admin\/_gallery\/preview\?/);
      await expect(page.getByRole('table')).toBeVisible();
    });
  });
}
