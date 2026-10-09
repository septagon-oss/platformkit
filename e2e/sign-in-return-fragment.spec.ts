import { expect, test } from '@playwright/test';
import { signIn, signInAs } from './steps/kernel';

test('signIn accepts a return address with a content fragment', async ({ page }) => {
  const destination = '/app/task/tasks#content';
  await signIn(page, destination);
  const arrived = new URL(page.url());
  expect(arrived.pathname + arrived.search + arrived.hash).toBe(destination);
});

test('signInAs accepts a return address with both query and fragment', async ({ browser }) => {
  const destination = '/app/task/tasks?limit=7&offset=0#content';
  const page = await signInAs(browser, {
    email: process.env.PLATFORMKIT_E2E_EMAIL!,
    password: process.env.PLATFORMKIT_E2E_PASSWORD!,
  }, destination);
  try {
    const arrived = new URL(page.url());
    expect(arrived.pathname + arrived.search + arrived.hash).toBe(destination);
  } finally {
    await page.context().close();
  }
});
