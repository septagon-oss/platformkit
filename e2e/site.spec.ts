import { expect, test } from '@playwright/test';
import { makePublisher, publishAs } from './steps/content';
import { signIn } from './steps/kernel';
import { openHome, signInDoor } from './steps/public';

// The public site: what an operator publishes through the admin's generated
// screens is what an anonymous visitor reads at the root of the host. The
// writes go through the JSON routes with the browser's session, because the
// journey is about the seam between the two modules and the shell, not about
// the forms, which admin-tasks.spec.ts already drives.

const publisherPass = 'a passphrase for the publisher';
const stamp = Date.now();
const slug = `welcome-${stamp}`;

test('a fresh site says nothing is published, and the home page appears once one is', async ({ page, browser }) => {
  await openHome(page);
  await expect(page.getByRole('heading', { name: 'Nothing published yet' })).toBeVisible();
  // The frame enters the workspace at its root: the address the public page is
  // allowed to offer is /app, and the root is what turns a visitor who has no
  // session towards the form. The step follows that entry rather than navigating to
  // the form, and insists the door carries the guarded address back in `next`.
  await signInDoor(page);
  await signIn(page);

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
