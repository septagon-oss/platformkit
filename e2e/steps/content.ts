import { expect, type Browser, type Page } from '@playwright/test';

export async function signInForContent(page: Page, email: string, password: string) {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
}

export async function fillContentForm(page: Page, fields: {
  slug: string;
  title: string;
  body: string;
  kind?: 'page' | 'post';
}) {
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Address name').fill(fields.slug);
  await page.getByLabel('Title').fill(fields.title);
  if (fields.kind) {
    await page.getByLabel('Type').selectOption(fields.kind);
  }
  await page.getByLabel('Body').fill(fields.body);
}

export async function publishContent(page: Page, id: string) {
  const response = await page.request.post(`/api/v1/content/contents/${id}/publish`);
  expect(response.status(), await response.text()).toBe(200);
}

// makePublisher invites a second person into the tenant, gives them a password
// and returns how to sign in as them. The content module refuses to let the
// author of a page publish it, so the journey that shows a published page needs
// two people: the one who wrote it and the one who decides it goes live.
export async function makePublisher(page: Page, email: string, password: string) {
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email, displayName: 'The publisher', roles: ['admin'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const { id } = await invited.json();
  const seeded = await page.request.post(`/api/v1/user/users/${id}/set-password`, { data: { password } });
  expect(seeded.status(), await seeded.text()).toBe(200);
}

// publishAs signs in as somebody else, in their own browser context, and
// publishes. The caller's session stays whoever it was.
export async function publishAs(browser: Browser, email: string, password: string, id: string) {
  const context = await browser.newContext();
  const publisher = await context.newPage();
  await signInForContent(publisher, email, password);
  await publishContent(publisher, id);
  await context.close();
}
