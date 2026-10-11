import { expect, test } from '@playwright/test';

// Rules 1 and 4 of the front door, in a browser and through the mail catcher: an
// administrator creates a person in the shell, the invitation arrives in Mailpit,
// its link carries the address this tenant is served at, and the person who opens
// it and chooses a password is signed in — nobody sets the password for them.
//
// The mail is read from Mailpit's own API (PLATFORMKIT_E2E_MAILPIT_URL, Mailpit's
// default address otherwise), by recipient, so the case reaches its assertions
// through what a delivered invitation carries and not through any sentence of the
// page. The tenant is served in pt-PT, so the form is found by its field's name and
// its submit button's type, never by a label.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

const mailpit = process.env.PLATFORMKIT_E2E_MAILPIT_URL ?? 'http://localhost:8025';

const person = {
  email: `invited-${Date.now()}@e2e.test`,
  password: 'e2e-invited-person-password',
  name: 'Invited From The Shell',
};

// The body of the first message Mailpit holds for `to`, or '' while it holds none.
async function mailFor(request: import('@playwright/test').APIRequestContext, to: string): Promise<string> {
  const found = await request
    .get(`${mailpit}/api/v1/search`, { params: { query: `to:"${to}"` } })
    .catch(() => null);
  if (!found || !found.ok()) return '';
  const { messages } = (await found.json()) as { messages?: { ID: string }[] };
  if (!messages || messages.length === 0) return '';
  const message = await request.get(`${mailpit}/api/v1/message/${messages[0].ID}`);
  if (!message.ok()) return '';
  const { Text, HTML } = (await message.json()) as { Text?: string; HTML?: string };
  return `${Text ?? ''}\n${HTML ?? ''}`;
}

test('a person created in the shell accepts the mailed invitation and lands signed in', async ({ page, browser, baseURL }) => {
  test.setTimeout(90_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);

  const created = await page.request.post('/api/v1/user/users', {
    data: { email: person.email, displayName: person.name },
  });
  expect(created.status(), await created.text()).toBe(201);

  let mail = '';
  await expect
    .poll(async () => (mail = await mailFor(page.request, person.email)), {
      message: `Mailpit at ${mailpit} holds an invitation for ${person.email}`,
      timeout: 30_000,
    })
    .not.toBe('');

  // The link is built from the address this tenant is served at, port included.
  const served = new URL(baseURL ?? 'http://localhost:8099').origin;
  const link = mail.match(/https?:\/\/[^\s"'<>]+\/app\/auth\/reset\?token=[A-Za-z0-9_\-=%]+/)?.[0] ?? '';
  expect(link.startsWith(`${served}/app/auth/reset?token=`), `the invitation's link: ${link}`).toBe(true);

  // The person's own browser opens it and chooses a password.
  const context = await browser.newContext();
  const as = await context.newPage();
  await as.goto(link);
  await as.locator('input[name="new"]').fill(person.password);
  await as.locator('form[data-auth-form="reset"] button[type="submit"]').click();
  await expect(as).not.toHaveURL(/\/app\/auth\/reset/);

  const me = await as.request.get('/api/v1/auth/me');
  expect(me.status(), await me.text()).toBe(200);

  await context.close();
});
