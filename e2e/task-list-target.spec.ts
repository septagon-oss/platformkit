import { expect, test } from '@playwright/test';
import { signInForContent } from './steps/content';

test('task row targets retain their height when a neighbouring assignment is hidden', async ({ page }) => {
  await signInForContent(page, process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test', process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  const ids: string[] = [];
  try {
    const specimen = await page.request.post('/api/v1/task/tasks', {
      data: {
        title: 'Design audit specimen',
        description: 'Long enough that the description column has a real width to report.',
        priority: 'high',
      },
    });
    expect(specimen.status()).toBe(201);
    const { id } = await specimen.json();
    ids.push(id);
    const neighbour = await page.request.post('/api/v1/task/tasks', {
      data: { title: 'Corrected title keeps its assignment' },
    });
    expect(neighbour.status()).toBe(201);
    const neighbourID = (await neighbour.json()).id;
    ids.push(neighbourID);
    const identity = await page.request.get('/api/v1/auth/me');
    expect(identity.status()).toBe(200);
    const assigned = await page.request.post(`/api/v1/task/tasks/${neighbourID}/assign`, {
      data: { assigneeId: (await identity.json()).userId },
    });
    expect(assigned.status()).toBe(200);

    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/app/task/tasks');
    await page.evaluate(() => document.fonts.ready);
    const link = page.locator(`a[href="/app/task/tasks/${id}"]`);
    await expect(link).toBeVisible();
    const box = await link.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.height, 'a task target must not depend on its title wrapping to a second line').toBeGreaterThanOrEqual(24);
  } finally {
    for (const id of ids) await page.request.delete(`/api/v1/task/tasks/${id}`);
  }
});
