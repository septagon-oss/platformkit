import { expect, test } from '@playwright/test';
import { signIn, signOut } from './steps/kernel';

test('a private Storybook read and an ended auth session require sign-in', async ({ page, request }) => {
  const index = '/app/admin/_gallery/storybook/index.json';
  const me = '/api/v1/auth/me';
  for (const path of [index, me]) {
    const response = await request.get(path);
    expect(response.status()).toBe(401);
    const refusal = await response.json();
    expect(refusal.status).toBe(401);
    expect(refusal.detail).toContain('AUTH_ANONYMOUS:');
  }

  await signIn(page);
  expect((await page.request.get(index)).status()).toBe(200);
  expect((await page.request.get(me)).status()).toBe(200);
  await signOut(page);

  for (const path of [index, me]) {
    const response = await page.request.get(path);
    expect(response.status()).toBe(401);
    const refusal = await response.json();
    expect(refusal.status).toBe(401);
    expect(refusal.detail).toContain('AUTH_ANONYMOUS:');
  }
});
