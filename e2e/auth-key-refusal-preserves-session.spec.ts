import { expect, test } from '@playwright/test';
import { signIn, signOut } from './steps/kernel';

test('auth keeps the same identity and session after refusing a key-only command', async ({ page }) => {
  await signIn(page);
  const me = '/api/v1/auth/me';
  const before = await page.request.get(me);
  expect(before.status()).toBe(200);
  const identity = await before.json();
  const cookies = await page.context().cookies();
  expect(cookies.length).toBeGreaterThan(0);

  const refusal = await page.request.post('/api/v1/auth/tokens/revoke-self', {
    headers: { Origin: new URL(page.url()).origin },
    data: {},
  });
  expect(refusal.status()).toBe(403);
  expect(await refusal.json()).toMatchObject({
    status: 403,
    detail: expect.stringContaining('AUTH_DENIED:'),
  });
  expect(refusal.headers()['www-authenticate']).toBeUndefined();
  expect(refusal.headers()['set-cookie']).toBeUndefined();

  const after = await page.request.get(me);
  expect(after.status()).toBe(200);
  expect(await after.json()).toEqual(identity);
  expect(await page.context().cookies()).toEqual(cookies);
  await signOut(page);
});
