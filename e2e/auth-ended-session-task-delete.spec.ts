import { expect, test } from '@playwright/test';
import { disposable, signIn, signOut } from './steps/kernel';

test('auth and task refuse deletion with an ended session cookie and preserve the row', async ({ page, playwright }) => {
  disposable();
  await signIn(page);
  const origin = new URL(page.url()).origin;
  const created = await page.request.post('/api/v1/task/tasks', {
    headers: { Origin: origin },
    data: { title: 'A task kept after its session ends', status: 'open', priority: 'normal' },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  expect(id).toMatch(/^[0-9a-f-]{36}$/);
  const path = `/api/v1/task/tasks/${id}`;
  const before = await page.request.get(path);
  expect(before.status()).toBe(200);
  const committed = await before.json();
  const storageState = await page.context().storageState();
  expect(storageState.cookies.length).toBeGreaterThan(0);
  const oldSession = await playwright.request.newContext({ baseURL: origin, storageState });
  try {
    expect((await oldSession.get('/api/v1/auth/me')).status()).toBe(200);
    await signOut(page);
    const refused = await oldSession.delete(path, { headers: { Origin: origin } });
    expect(refused.status()).toBe(401);
    const problem = await refused.json();
    expect(problem).toMatchObject({ status: 401, detail: expect.stringContaining('AUTH_ANONYMOUS:') });
    expect(problem).not.toHaveProperty('id');
    expect(problem).not.toHaveProperty('revision');
    expect(JSON.stringify(problem)).not.toContain(committed.title);
    await signIn(page);
    const after = await page.request.get(path);
    expect(after.status()).toBe(200);
    expect(await after.json()).toEqual(committed);
  } finally {
    await oldSession.dispose();
  }
});
