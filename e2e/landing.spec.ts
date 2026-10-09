import { expect, test } from '@playwright/test';

// Rule 5's journey on a browser: a person who was invited, given a password by the
// administrator's own command because the invitation link goes to a mailbox no
// browser can read, signs in — and is met by a page that has something on it.
//
// What the walkthrough of record scored zero on in twelve of the eighteen apps was
// not the address a sign-in landed on; it was what stood behind it: a person whose
// role opened the practice screens was welcomed by a heading, a health alert and a
// grid of count cards with no card in it, whose only other door was Health. So the
// assertions here are the doors this person is actually offered — the sidebar names
// the screen their role opens, the landing page carries a card that leads there, and
// the address behind the card answers them 200 — rather than a sentence, because
// this tenant is served in pt-PT (scripts/e2e.sh passes --language) and an English
// word in a spec is a sentence that breaks the day the copy moves into a catalogue.
//
// The role is the task module's own declaration (modules/task declares coordinator
// in its manifest, and the composition seeds declaredRoles into every new tenant),
// which is what makes this rule 5 and rule 3 in one journey: the grant that opens
// the screen has to be one a module declared, not one this fixture invented.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

const person = {
  email: `coordinator-${Date.now()}@e2e.test`,
  password: 'e2e-coordinator-landing-password',
  name: 'Landing Coordinator',
};

// The screen the coordinator role is declared to open, in the workspace.
const desk = '/app/task/tasks';

test('a person whose role opens one screen is landed on a page that offers it', async ({ page, browser }) => {
  test.setTimeout(90_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: person.email, displayName: person.name, roles: ['coordinator'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, {
    data: { password: person.password },
  });
  expect(activated.status(), await activated.text()).toBe(200);

  // Their own browser, not this one: the landing is what a person sees, and a
  // second context is the only way to see it without the administrator's grants.
  const context = await browser.newContext();
  const as = await context.newPage();
  await as.goto('/app/admin/login');
  await as.getByLabel('Email').fill(person.email);
  await as.getByLabel('Password').fill(person.password);
  await as.getByRole('button', { name: 'Sign in' }).click();
  await expect(as).toHaveURL(/\/app$/);

  // The sidebar names the door their role opens. A menu whose only items are the
  // landing page and Health is the walkthrough's zero, and it is a rendered list,
  // so this is the assertion that says the composition reached the menu.
  const menu = as.getByRole('navigation', { name: 'Admin navigation' }).first();
  await expect(menu.getByRole('link', { name: 'Tasks' })).toHaveAttribute('href', desk);

  // And the page they landed on offers the same door, not an empty grid: the card
  // for what their role may count. Clickable cards are anchors with the screen as
  // their href, which is what makes this a door and not a caption.
  // Scoped to the page under the heading: <main> is the landing's own body and the
  // sidebar is an <aside>, so this cannot quietly be the menu link measured above.
  const card = as.getByRole('main').getByRole('link', { name: /task/i }).first();
  await expect(card).toHaveAttribute('href', desk);

  await card.click();
  await expect(as).toHaveURL(desk);
  // The desk itself, answered to them: a table, not a refusal page.
  await expect(as.getByRole('table')).toBeVisible();

  await context.close();
});
