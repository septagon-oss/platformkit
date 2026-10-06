import { expect, test } from '@playwright/test';

test('an invited person without a display name is linked by address', async ({ page }) => {
  test.setTimeout(90_000);
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const email = `unnamed-${Date.now()}@e2e.test`;
  const invited = await page.request.post('/api/v1/user/invitations', { data: { email } });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  expect(id).toBeTruthy();

  await page.goto('/app/user/users');
  await expect(page.locator(`table a[href="/app/user/users/${id}"]`)).toHaveText(email);
});
