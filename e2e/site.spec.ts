import { expect, test } from '@playwright/test';

// The public site: what an operator publishes through the admin's generated
// screens is what an anonymous visitor reads at the root of the host. The
// writes go through the JSON routes with the browser's session, because the
// journey is about the seam between the two modules and the shell, not about
// the forms, which admin-tasks.spec.ts already drives.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const stamp = Date.now();
const slug = `welcome-${stamp}`;

test('a fresh site says nothing is published, and the home page appears once one is', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Nothing published yet' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sign in to the admin' })).toHaveAttribute('href', '/admin/login');

  await page.goto('/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/admin$/);

  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug, title: `Welcome ${stamp}`, kind: 'page', body: `# Hello\n\nThis is **home** number ${stamp}.` },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  const published = await page.request.post(`/api/v1/content/contents/${id}/publish`);
  expect(published.ok(), await published.text()).toBeTruthy();
  const settings = await page.request.put('/api/v1/site/settings', {
    data: { title: `Acme ${stamp}`, tagline: 'From the workshop', homeSlug: slug, theme: 'light', primaryColor: '#2563eb',
      nav: [{ label: 'Welcome', path: `/${slug}` }] },
  });
  expect(settings.ok(), await settings.text()).toBeTruthy();

  await page.goto('/');
  await expect(page).toHaveTitle(new RegExp(`Welcome ${stamp}`));
  await expect(page.getByRole('heading', { name: `Welcome ${stamp}` })).toBeVisible();
  await expect(page.getByText(`home number ${stamp}`)).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByRole('navigation', { name: 'Site navigation' }).getByRole('link', { name: 'Welcome' }).click();
  await expect(page).toHaveURL(new RegExp(`/${slug}$`));

  const missing = await page.goto('/no-such-page');
  expect(missing?.status()).toBe(404);
  await expect(page.getByRole('link', { name: 'Back to the site' })).toBeVisible();
});
