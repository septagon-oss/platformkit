import { expect, test } from '@playwright/test';
import { signIn, signInAs } from './steps/kernel';

// The guarded resource carries its list position through the sign-in door.
const destination = '/app/task/tasks?limit=7&offset=0';

test('signIn accepts a return address with its query', async ({ page }) => {
  await signIn(page, destination);
  const arrived = new URL(page.url());
  expect(arrived.pathname + arrived.search).toBe(destination);
});

test('signInAs accepts a return address with its query', async ({ browser }) => {
  const page = await signInAs(browser, {
    email: process.env.PLATFORMKIT_E2E_EMAIL!,
    password: process.env.PLATFORMKIT_E2E_PASSWORD!,
  }, destination);
  try {
    const arrived = new URL(page.url());
    expect(arrived.pathname + arrived.search).toBe(destination);
  } finally {
    await page.context().close();
  }
});
