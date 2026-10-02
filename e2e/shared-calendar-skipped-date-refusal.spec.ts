import { expect, test } from '@playwright/test';

// Exercise the same editable, authenticated Gallery path that exposed the
// skipped-date loop. Use the event identity as the positive reachability check;
// a localized error message or a particular failure status is not the contract.
for (const language of ['en', 'pt-PT']) {
  test(`calendar refuses skipped dates without partial output and recovers (${language})`, async ({ page }) => {
    await page.goto('/app/admin/login');
    await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
    await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page).toHaveURL(/\/app$/);

    const props = {
      timeZone: 'UTC', timeZoneLabel: 'UTC',
      rangeStartDate: '2011-12-28', rangeEndDate: '2011-12-31',
      events: [{
        id: 'review-boundary-event', title: 'Boundary', timeText: '28',
        statusLabel: 'Available', allDay: true,
        startDate: '2011-12-28', endDate: '2011-12-29',
      }],
      dateStrip: {
        label: 'Date', selectedDate: '2011-12-28',
        days: [{ date: '2011-12-28', label: '28', href: '/calendar?date=2011-12-28' }],
      },
    };
    const preview = () => '/app/admin/_gallery/preview?' + new URLSearchParams({
      example: `pk-ui.component.calendar/default-${language}`,
      theme: 'light', props: JSON.stringify(props),
    });
    const normal = await page.goto(preview());
    expect(normal?.status()).toBe(200);
    await expect(page.locator('[data-calendar-event="review-boundary-event"]')).toHaveCount(1);

    props.timeZone = props.timeZoneLabel = 'Pacific/Apia';
    for (const end of ['2011-12-31', '2011-12-30']) {
      props.rangeEndDate = end;
      const refused = await page.request.get(preview(), { timeout: 2_000 });
      expect(refused.status()).toBeGreaterThanOrEqual(400);
      expect(await refused.text()).not.toContain('data-calendar-event=');
    }

    props.timeZone = props.timeZoneLabel = 'UTC';
    const recovered = await page.goto(preview());
    expect(recovered?.status()).toBe(200);
    await expect(page.locator('[data-calendar-event="review-boundary-event"]')).toHaveCount(1);
  });
}
