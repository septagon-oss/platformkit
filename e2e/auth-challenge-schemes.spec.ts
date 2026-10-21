import { expect, test } from '@playwright/test';
import { signIn } from './steps/kernel';

test('auth challenges an unrecognised key and refuses a session at the key-only command', async ({ page, request }) => {
  for (const path of ['/api/v1/auth/me', '/api/v1/app/resources', '/api/v1/auth/roles']) {
    const response = await request.get(path);
    expect(response.status(), path).toBe(401);
    expect(response.headers()['www-authenticate'], path).toBeUndefined();
    expect(await response.json()).toMatchObject({ status: 401, detail: expect.stringContaining('AUTH_ANONYMOUS:') });
  }

  const revoke = '/api/v1/auth/tokens/revoke-self';
  for (const [authorization, challenge] of [
    ['', 'Bearer'],
    ['Bearer unrecognised-key', 'Bearer error="invalid_token"'],
  ]) {
    const response = await request.post(revoke, {
      headers: authorization ? { Authorization: authorization } : {},
      data: {},
    });
    expect(response.status()).toBe(401);
    expect(response.headers()['www-authenticate']).toBe(challenge);
    expect(await response.json()).toMatchObject({ status: 401, detail: expect.stringContaining('AUTH_ANONYMOUS:') });
  }

  await signIn(page);
  const response = await page.request.post(revoke, {
    headers: { Origin: new URL(page.url()).origin },
    data: {},
  });
  expect(response.status()).toBe(403);
  expect(response.headers()['www-authenticate']).toBeUndefined();
  expect(await response.json()).toMatchObject({ status: 403, detail: expect.stringContaining('AUTH_DENIED:') });
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(200);
});
