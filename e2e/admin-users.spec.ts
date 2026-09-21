import { expect, test } from '@playwright/test';

// Gate 10. The floor under a tenant's last administrator, driven the way the
// person who would trip it does: signed in as the only administrator this
// tenant has, on the generated user screen, pressing the button.
//
// Before, every one of these answered 2xx and the next page load answered 403
// AUTH_DENIED — the roles screen and the users screen at once (and the tenant
// list too, until the control plane moved off a customer's host) — and the delete
// case left the next test unable to sign in at all. The fixture's
// administrator is the bootstrap one and the only one, and its tenant is the
// operator's, which is the case with no repair: the route that puts an
// administrator into a tenant is guarded by the operator permission this
// account holds through the role it was about to give up.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('the last administrator cannot delete themselves off the user screen', async ({ page }) => {
  await page.getByRole('link', { name: 'Users' }).first().click();
  await expect(page).toHaveURL(/\/app\/user\/users$/);
  // The row is reached by id rather than by the link's text, because what the
  // generated list calls a row is kit/entity/display's business and this case
  // is about the button on the page it leads to.
  const identity = await page.request.get('/api/v1/auth/me');
  expect(identity.status()).toBe(200);
  await page.goto(`/app/user/users/${(await identity.json()).userId}`);

  // Two clicks: the button and the shell's confirm dialog.
  await page.getByRole('button', { name: 'Delete' }).click();
  const dialog = page.locator('#pk-confirm');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete' }).click();

  await expect(page.getByRole('alert').first())
    .toContainText('would leave nobody who can sign in and administer this tenant');

  // And the refusal rolled the whole transaction back: the account is still
  // there, still signed in, still able to read the screen it came from.
  await page.goto('/app/user/users');
  await expect(page.getByText(email).first()).toBeVisible();
});

test('the last administrator cannot be stripped or deactivated through the commands', async ({ page }) => {
  const identity = await page.request.get('/api/v1/auth/me');
  expect(identity.status()).toBe(200);
  const self = (await identity.json()).userId;

  const stripped = await page.request.post(`/api/v1/user/users/${self}/roles`, { data: { roles: [] } });
  expect(stripped.status()).toBe(422);
  expect(await stripped.text()).toContain('would leave nobody who can sign in and administer');

  const stopped = await page.request.post(`/api/v1/user/users/${self}/deactivate`);
  expect(stopped.status()).toBe(422);

  // The proof that the refusals mattered: everything the lockout took away is
  // still answering for this session.
  expect((await page.request.get('/api/v1/auth/roles')).status()).toBe(200);
  expect((await page.request.get('/api/v1/user/users')).status()).toBe(200);
  // The tenant list is not a door a customer's host has, lockout or none: the
  // control plane is served at the installation's own host and nowhere else, so
  // the address the old release answered here at the byte what an unmounted
  // address answers. Asserted rather than dropped, because it is the difference
  // between a refusal that came from this lockout and one that came from the
  // surface — and only the first was supposed to be negotiable.
  expect((await page.request.get('/api/v1/tenant/tenants')).status()).toBe(404);
});
