import { expect, test } from '@playwright/test';

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('an editor saves Markdown and sees shared prose on the detail view', async ({ page }) => {
  const slug = `richtext-${Date.now()}`;
  const markdown = '## Opening hours  \n\n- Monday\n- Tuesday\n\n[Website](https://example.com)\n\n```go\nfmt.Println(1)\n```';
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Slug').fill(slug);
  await page.getByLabel('Title').fill('Opening hours');
  await page.getByLabel('Kind').selectOption('page');
  await page.getByLabel('Body').fill(markdown);
  await expect(page.getByText('Formatting help')).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const prose = page.locator('[data-component="prose"]');
  await expect(prose.locator('#pk-opening-hours')).toHaveText('Opening hours');
  await expect(prose.locator('li')).toHaveCount(2);
  await expect(prose.getByRole('link', { name: 'Website' })).toHaveAttribute('rel', 'noopener noreferrer nofollow ugc');
  await expect(prose.locator('pre code')).toContainText('fmt.Println(1)');
  const id = page.url().split('/').at(-1);
  const stored = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(stored.status()).toBe(200);
  expect((await stored.json()).body).toBe(markdown.replace('hours  \n', 'hours\n') + '\n');
});

test('a refused construct keeps the Markdown and identifies each line', async ({ page }) => {
  const markdown = '<script>alert(1)</script>\n![x](https://example.com/x.png)';
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Slug').fill(`refused-${Date.now()}`);
  await page.getByLabel('Title').fill('Refused example');
  await page.getByLabel('Body').fill(markdown);
  const refused = page.waitForResponse(response => new URL(response.url()).pathname === '/app/content/contents' && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Save' }).click();
  expect((await refused).status()).toBe(422);
  await expect(page.getByLabel('Body')).toHaveValue(markdown);
  const summary = page.getByRole('navigation', { name: 'Fields to correct' });
  await expect(summary.getByRole('link', { name: /Body/ })).toHaveAttribute('href', /^#.+/);
  await expect(page.getByText(/line 1: raw HTML/i).first()).toBeVisible();
  await expect(page.getByText(/line 2: image source/i).first()).toBeVisible();
  await expect(page.getByText(/Upload the image so it is stored with this site/).first()).toBeVisible();
});

test('a published page serves the same prose and a bounded meta description', async ({ page }) => {
  const slug = `published-prose-${Date.now()}`;
  const body = '## Introduction\n\n' + 'Readable words. '.repeat(20);
  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug, title: 'Published prose', kind: 'page', body },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  const published = await page.request.post(`/api/v1/content/contents/${id}/publish`);
  expect(published.status(), await published.text()).toBe(200);
  await page.goto(`/app/content/contents/${id}`);
  const adminProse = await page.locator('[data-component="prose"]').innerHTML();
  const publicRead = await page.request.get(`/api/v1/public/content/contents/${slug}`);
  expect(publicRead.status(), await publicRead.text()).toBe(200);
  expect((await publicRead.json()).html.trim()).toBe(adminProse.trim());
  await page.goto(`/${slug}`);
  await expect(page.locator('[data-component="prose"]')).toContainText('Readable words');
  const description = await page.locator('meta[name="description"]').getAttribute('content');
  expect(description?.length).toBeLessThanOrEqual(160);
  expect(description).not.toMatch(/\bReadab$/);
});
