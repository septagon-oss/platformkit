import { expect, test } from '@playwright/test';

// The brief's journey, driven by two browsers: a person without the grant is
// refused a screen, is told from that screen which grant is missing and which role
// hands it out, asks from the same page, the notice reaches an administrator who
// grants it, and the person's own retry answers.
//
// Two things about this fixture are load-bearing for how the member is made. The
// tenant this installation boots with is served in pt-PT (scripts/e2e.sh passes
// --language), so nothing here asserts an English sentence — the assertions are the
// form, the address it posts to, the hidden fields the refusal wrote, and the
// verdict at each door. And the only door that lets a new person choose a password
// is the link the invitation mails, which goes to an in-memory mailbox no browser
// can read; the administrator therefore sets it through the person's own command,
// which is the same command that flow ends at. Everything after the account exists
// is what the two people do with their own sessions.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

const member = {
  email: `refused-${Date.now()}@e2e.test`,
  password: 'e2e-refused-member-password',
  name: 'Refused Member',
};

// refusedScreen is a screen the tenant's member role grants nothing on, which is
// the state the brief's walkthrough starts in.
const refusedScreen = '/app/task/tasks';
// A role name the auth module accepts: a lower-case identifier, digits allowed,
// and one per run so two runs over the same fixture cannot collide.
const role = `readers_${Date.now()}`;

test('a refused person asks from the page that refused them and is granted', async ({ page, browser }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  // A role that reads tasks and nothing else, so the grant the person asks for is
  // a thing that exists to be handed out, and handing it out is not making them an
  // administrator.
  const created = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['task:read'] } });
  expect(created.status(), await created.text()).toBe(200);

  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: member.email, displayName: member.name, roles: ['member'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  expect(id).toBeTruthy();

  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, { data: { password: member.password } });
  expect(activated.status(), await activated.text()).toBe(200);

  // The person who holds nothing, in a browser of their own.
  const context = await browser.newContext();
  const person = await context.newPage();
  await person.goto('/app/admin/login');
  await person.getByLabel('Email').fill(member.email);
  await person.getByLabel('Password').fill(member.password);
  await person.getByRole('button', { name: 'Sign in' }).click();
  await expect(person).toHaveURL(/\/app$/);

  const refusal = await person.goto(refusedScreen);
  expect(refusal?.status(), 'the member was not refused, so this journey has no start').toBe(403);

  // The ask control the refusal page owes: the grant and the address, written into
  // the form by the page that refused, posting to the door that is mounted.
  const form = person.locator('form[action="/app/access-request"]');
  await expect(form.locator('input[name="permission"][value="task:read"]')).toHaveCount(1);
  await expect(form.locator('input[name="path"]')).toHaveCount(1);

  // The way on is followed, not admired: an address that answers 404 is the dead
  // end this journey exists to close, and the answer at the address is the only
  // thing that tells the two apart.
  const back = person.locator('a[href="/app"]').first();
  await expect(back).toHaveCount(1);  const way = (await back.getAttribute('href')) ?? '';
  expect(way, 'the refusal offers no way on').not.toBe('');
  const onward = await person.goto(way);
  expect(onward?.status(), `the refusal's way on ${way} answers nothing`).toBe(200);
  await expect(person.locator('h1, h2').first()).toBeVisible();

  // One ask, submitted the way the page submits it.
  await person.goto(refusedScreen);
  await form.locator('button[type="submit"]').click();
  await expect(person).toHaveURL(/\/app\/access-request\/sent$/);
  // A success alert is role=status: ui/components/alert.go gives role=alert to
  // the tones that interrupt a reader and status to the ones that inform them.
  await expect(person.locator('[role="status"]')).toContainText(/sent|told/i);

  // The notice is in the tenant's own bell, naming the person and the grant, and
  // it is the only thing the ask wrote besides its trail entry: the person's next
  // request is refused exactly as the first one was.
  const bell = await page.request.get('/api/v1/notification/notifications');
  expect(bell.status()).toBe(200);
  const told = (await bell.json()) as { items?: { title?: string; body?: string }[] };
  const notice = (told.items ?? []).find((n) => n.title === 'Access requested');
  expect(notice, `no access ask in the administrator's bell: ${JSON.stringify(told)}`).toBeTruthy();
  expect(notice?.body).toContain('task:read');
  expect(notice?.body).toContain(id);

  const still = await person.goto(refusedScreen);
  expect(still?.status(), 'an ask is not a grant').toBe(403);

  // The grant, through the person's own command — the same door the notice's deep
  // link leads to.
  const granted = await page.request.post(`/api/v1/user/users/${id}/roles`, { data: { roles: ['member', role] } });
  expect(granted.status(), await granted.text()).toBe(200);

  const retry = await person.goto(refusedScreen);
  expect(retry?.status(), 'the person was granted and is still refused').toBe(200);

  await context.close();
});
