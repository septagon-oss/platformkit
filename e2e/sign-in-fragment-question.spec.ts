import { expect, test } from '@playwright/test';
import { disposable, signIn, signInAs } from './steps/kernel';

test('signIn preserves a question mark inside the return fragment', async ({ page }) => {
  const destination = '/app/task/tasks#content?section=tasks';
  await signIn(page, destination);
  const arrived = new URL(page.url());
  expect(arrived.pathname).toBe('/app/task/tasks');
  expect(arrived.search).toBe('');
  expect(arrived.hash).toBe('#content?section=tasks');
});

test('signInAs distinguishes the query from a question mark inside the fragment', async ({ browser }) => {
  const destination = '/app/task/tasks?limit=7&offset=0#content?section=tasks';
  const page = await signInAs(browser, disposable(), destination);
  try {
    const arrived = new URL(page.url());
    expect(arrived.pathname).toBe('/app/task/tasks');
    expect(arrived.search).toBe('?limit=7&offset=0');
    expect(arrived.hash).toBe('#content?section=tasks');
  } finally {
    await page.context().close();
  }
});
