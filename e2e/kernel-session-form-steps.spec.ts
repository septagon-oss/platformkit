import { expect, test } from '@playwright/test';
import { declaresLanguage, disposable, moment, openSession, save, structure, useTheme } from './steps/kernel';

test('an API session stays in its own context and saves through the generated form', async ({ browser }) => {
  const person = disposable();
  const session = await openSession(browser, person, { width: 1440, reducedMotion: 'reduce' });
  const outsider = await browser.newContext({ baseURL: process.env.PLATFORMKIT_E2E_URL });
  try {
    expect((await outsider.request.get('/api/v1/auth/me')).status()).toBe(403);
    expect((await session.context.request.get('/api/v1/auth/me')).status()).toBe(200);
    await useTheme(session.page, 'dark');
    await session.page.goto('/app/task/tasks/new');
    await expect(session.page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await declaresLanguage(session.page, 'en');
    await structure(session.page);
    const title = `Published form step ${Date.now()}`;
    await session.page.getByLabel('Title').fill(title);
    await session.page.getByLabel('Due At').fill(moment(60));
    await save(session.page);
    await expect(session.page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    await expect(session.page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);
  } finally {
    await session.context.close();
    await outsider.close();
  }
});
