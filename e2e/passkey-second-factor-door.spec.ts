import { expect, test, type Page } from '@playwright/test';

// The passkey as the second factor after a password, done the way a person does
// it: by clicking. The sessions screen offers "Add a passkey" to every signed-in
// person, and from that moment the module refuses the bare password for them. A
// tenant that never opened the usernameless door — every tenant, until its
// administrator says otherwise — must still leave that person a control on the
// sign-in page that finishes the sign-in the password began. The case reaches its
// assertion through what the fixed page does (the URL it lands at, the session
// /api/v1/auth/me answers for), never through the wording of a refusal.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? '',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const settings = '/api/v1/auth/settings/passkey-sign-in';

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}

test('a person who added a passkey on the sessions screen finishes a password sign-in by clicking it', async ({ page, browser }) => {
  test.setTimeout(90_000);
  await signIn(page, admin.email, admin.password);
  await expect(page).toHaveURL(/\/app$/);
  // The tenant every installation starts as: the usernameless door shut.
  const shut = await page.request.post(settings, { data: { enabled: false } });
  expect(shut.status(), await shut.text()).toBe(200);

  const email = `passkey-second-factor-${Date.now()}@e2e.test`;
  const password = 'a-second-factor-door-password';
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email, displayName: 'Passkey second factor', roles: ['member'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  expect((await page.request.post(`/api/v1/user/users/${id}/set-password`, {
    data: { password },
  })).status()).toBe(200);

  const context = await browser.newContext();
  const person = await context.newPage();
  const cdp = await context.newCDPSession(person);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2', transport: 'internal', hasResidentKey: true,
      hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true,
    },
  });

  await signIn(person, email, password);
  await expect(person).toHaveURL(/\/app$/);
  await person.goto('/app/auth/sessions');
  await person.getByLabel('What this device is called').fill('Second factor laptop');
  await person.getByRole('button', { name: 'Add a passkey' }).click();
  await expect(person.locator('[data-auth-message]')).toBeVisible();

  await person.locator('[data-sign-out]').click();
  await expect(person).toHaveURL(/\/app\/admin\/login/);
  await signIn(person, email, password);
  // The password was right and one thing is missing; the page that says so
  // offers the passkey the person just added. Whichever passkey control the
  // page draws, one of them, clicked, signs the person in.
  await person.waitForLoadState('networkidle');
  const controls = person.getByRole('button', { name: /passkey/i });
  await controls.first().waitFor({ state: 'visible', timeout: 10_000 }).catch(() => undefined);
  const count = await controls.count();
  expect(count, 'the sign-in page offers no passkey control at all').toBeGreaterThan(0);
  let signedIn = false;
  for (let i = 0; i < count && !signedIn; i++) {
    if (!(await controls.nth(i).isVisible())) continue;
    await controls.nth(i).click();
    signedIn = await person.waitForURL(/\/app$/, { timeout: 10_000 }).then(() => true, () => false);
  }
  expect(signedIn, 'no passkey control on the sign-in page finished the password sign-in').toBe(true);
  expect((await person.request.get('/api/v1/auth/me')).status()).toBe(200);

  await context.close();
});
