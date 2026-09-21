import { expect, test } from '@playwright/test';

// The nav entry modules/auth declares, followed the way a person follows it.
//
// It used to lead to a 404 and the shell warned about it at boot, once, where
// nobody reading a menu would see it. This is the other half of that fix: the
// link is in the sidebar, it opens the screen, and a tick on it changes what a
// role grants.
//
// The journey edits `member` and never `admin`. The signed-in administrator
// holds the wildcard through `admin`, so a spec that cleared that role's boxes
// would sign itself out of the screen it is testing.
//
// The boxes are ticked through their own labels. The input itself is visually
// hidden — the indicator beside it is what a person sees and clicks — which is
// the same thing checkbox-state.spec.ts does for the same reason.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('the Roles entry in the sidebar opens a screen that changes what a role grants', async ({ page }) => {
  await page.getByRole('link', { name: 'Roles' }).first().click();
  await expect(page).toHaveURL(/\/app\/auth\/roles$/);
  await expect(page.getByRole('heading', { name: 'Roles', exact: true })).toBeVisible();

  // The two roles every tenant is seeded with, each with a form of its own.
  await expect(page.getByRole('form', { name: 'What admin grants' })).toBeVisible();
  const member = page.getByRole('form', { name: 'What member grants' });
  await expect(member).toBeVisible();

  // member starts with nothing, which is what that role is for.
  const read = page.locator('#pk-role-member-task-read');
  await expect(read).not.toBeChecked();

  await page.locator('label[for="pk-role-member-task-read"]').click();
  await expect(read).toBeChecked();
  await member.getByRole('button', { name: 'Save member' }).click();
  await expect(page).toHaveURL(/\/app\/auth\/roles$/);

  // The write landed, and the screen renders it back from the row rather than
  // from what the browser still had on screen.
  await page.reload();
  await expect(page.locator('#pk-role-member-task-read')).toBeChecked();
  // admin's own grant is untouched: one form saved one role.
  await expect(page.locator('#pk-role-admin-everything')).toBeChecked();
});

test('a role name the module refuses comes back on the screen rather than on a fault page', async ({ page }) => {
  await page.goto('/app/auth/roles');
  const create = page.getByRole('form', { name: 'A new role' });
  // A leading digit is not a lower-case identifier. The browser's own pattern
  // check is removed first on purpose: what is being tested is the server's
  // refusal and where it is shown, not the control's hint.
  const name = create.getByLabel('Name');
  await name.evaluate((el: HTMLInputElement) => el.removeAttribute('pattern'));
  await name.fill('1editor');
  await create.getByRole('button', { name: 'Create the role' }).click();

  await expect(page.getByText(/is not a lower-case identifier/)).toBeVisible();
  // The screen came back, not the shell's fault page: the other roles are still
  // there to correct, and kit/crud's own prefix is not on the message.
  await expect(page.getByRole('form', { name: 'What member grants' })).toBeVisible();
  await expect(page.getByText(/crud: invalid/)).toHaveCount(0);
});

test('the last role that can administer roles refuses to be emptied', async ({ page }) => {
  await page.goto('/app/auth/roles');
  // admin is the only seeded role with the wildcard, so it is the only one that
  // grants role:manage. Unticking that one box and pressing save is the whole
  // of the mistake: afterwards nobody in this tenant could open this screen
  // again, and the operator of the installation could not repair it either.
  const owner = page.getByRole('form', { name: 'What admin grants' });
  await page.locator('label[for="pk-role-admin-everything"]').click();
  await expect(page.locator('#pk-role-admin-everything')).not.toBeChecked();
  await owner.getByRole('button', { name: 'Save admin' }).click();

  await expect(page.getByText(/last role that grants role:manage/)).toBeVisible();
  // Nothing was written: the box is ticked again because the row still says so.
  await page.reload();
  await expect(page.locator('#pk-role-admin-everything')).toBeChecked();
});
