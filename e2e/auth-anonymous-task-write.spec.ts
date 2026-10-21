import { expect, test } from '@playwright/test';
import { disposable, signIn, signOut } from './steps/kernel';

test('auth refuses an anonymous task edit with 401 and preserves the committed row', async ({ page }) => {
  disposable();
  await signIn(page);
  const created = await page.request.post('/api/v1/task/tasks', {
    headers: { Origin: new URL(page.url()).origin },
    data: { title: 'A task protected by its session', status: 'open', priority: 'normal' },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  expect(id).toMatch(/^[0-9a-f-]{36}$/);
  const path = `/api/v1/task/tasks/${id}`;
  const before = await page.request.get(path);
  expect(before.status()).toBe(200);
  const committed = await before.json();

  await signOut(page);
  const refused = await page.request.patch(path, {
    headers: { Origin: new URL(page.url()).origin },
    data: { title: 'An anonymous replacement', revision: committed.revision },
  });
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
});
