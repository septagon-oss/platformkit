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

// The first lines are the tenant's first day: `platformkit bootstrap` created
// the tenant through the tenant module, whose creation hook applied the starter seed
// (apps/platformkit/seed/starter), so the visitor lands on a page and not on an
// apology. The empty state this case used to assert is still what a site with a home
// slug that names nothing published shows — modules/web's own
// TestSiteRefusesWhatItCannotServe covers those words — but it is no longer what a
// new installation shows.
test('a new tenant opens on its starter home, and a published page takes its place', async ({ page, browser }) => {
  await openHome(page);
  await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible();
  await expect(page.getByText('This site is served by PlatformKit')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Welcome' })).toBeVisible();

  // The workspace is entered at its root: /app is the address the public frame is
  // allowed to offer (webcontracts.Links.SignIn, pinned by
  // TestThePublicFrameLinksOnlyTheWorkspaceRoot), and the root is what turns a
  // visitor with no session towards the form. modules/web carries that door on every
  // public page now — public-frame-links-workspace-root.spec.ts is the browser's
  // evidence for it — so the journey follows the entry the page offers rather than
  // naming the form's address itself: the step requires the entry to exist and the
  // guard to carry /app back in `next`, which is what makes an entry a door. The
  // form and the landing it answers for are main's own: next=%2Fapp, /app after.
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
