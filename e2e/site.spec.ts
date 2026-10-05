import { expect, test } from '@playwright/test';
import { makePublisher, publishAs } from './steps/content';

// The public site: what an operator publishes through the admin's generated
// screens is what an anonymous visitor reads at the root of the host. The
// writes go through the JSON routes with the browser's session, because the
// journey is about the seam between the two modules and the shell, not about
// the forms, which admin-tasks.spec.ts already drives.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const publisherPass = 'a passphrase for the publisher';
const stamp = Date.now();
const slug = `welcome-${stamp}`;

test('a fresh site says nothing is published, and the home page appears once one is', async ({ page, browser }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Nothing published yet' })).toBeVisible();
  // The site's way into the workspace is the workspace root, and nothing deeper:
  // a public page may name /app and no screen behind it (TestLocalizedCompositionKeepsSurfaceBoundaries
  // reads the published frame's own hrefs). An anonymous browser that asks /app is taken
  // to the door with the root remembered as `next` — surfaces.spec.ts holds that redirect
  // — so the person who follows this link arrives at the same form as ever, one hop
  // through the address the public page is allowed to print.
  const signInLink = page.getByRole('link', { name: 'Sign in to the admin' });
  await expect(signInLink).toHaveAttribute('href', '/app');

  await signInLink.click();
  await expect(page.locator('[data-login-form]')).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug, title: `Welcome ${stamp}`, kind: 'page', body: `## Hello\n\nThis is **home** number ${stamp}.` },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  // Writing the home page and putting it in front of the host are two people's
  // decisions: the content module refuses the author as publisher, so the journey
  // invites the one who publishes. See e2e/steps/content.ts.
  const publisher = `publisher-${stamp}@e2e.test`;
  await makePublisher(page, publisher, publisherPass);
  await publishAs(browser, publisher, publisherPass, id);
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
