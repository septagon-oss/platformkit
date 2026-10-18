import { expect, test } from '@playwright/test';
import { disposable, moment, openSession, save } from './steps/kernel';

test('kernel form steps save the task target separately from its response deadline', async ({ browser }) => {
  const session = await openSession(browser, disposable());
  try {
    await session.page.goto('/app/task/tasks/new');
    const target = moment(60);
    const deadline = moment(120);
    const title = `Separate task dates ${Date.now()}`;
    await session.page.getByLabel('Title').fill(title);
    await session.page.getByLabel('Due', { exact: true }).fill(target);
    await session.page.getByLabel('Response due', { exact: true }).fill(deadline);
    await save(session.page);
    await expect(session.page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    await expect(session.page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);
    const row = session.page.url();
    await session.page.goto(`${row}/edit`);
    await expect(session.page.getByLabel('Due', { exact: true })).toHaveValue(target);
    await expect(session.page.getByLabel('Response due', { exact: true })).toHaveValue(deadline);
  } finally {
    await session.context.close();
  }
});
