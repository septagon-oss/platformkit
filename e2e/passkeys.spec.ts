import { expect, test, type CDPSession, type Page } from '@playwright/test';

// The passkey journey, driven by a browser and an emulated authenticator.
//
// Chromium speaks CTAP2 over CDP: WebAuthn.addVirtualAuthenticator installs a
// device the page can prompt, holding real key material, that will not sign for
// a relying party it was not enrolled at. That is the claim this file exists for
// — a passkey made for one host does not answer at another, and it is the
// client, and not this repository, that enforces it.
//
// The ceremony legs are the auth module's published routes, called from the page
// with the page's own cookie jar: the browser half (navigator.credentials) and
// the server half (the two POSTs) both run, and nothing between them is
// synthesised. The journey stops at the ceremony: the screens and the operator's
// switch behind the usernameless door are named as the remaining edits in the
// delivery report, and the brief's second tenant, served on a second host in the
// same browser, is among them — `platformkit bootstrap` refuses to make a second
// tenant, so the fixture has to be given one first.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const beginEnrolment = '/api/v1/auth/factors/passkey/begin';
const finishEnrolment = '/api/v1/auth/factors/passkey/finish';
const beginSignIn = '/api/v1/auth/login/passkey/begin';

// A virtual authenticator per page: a resident credential, user verification
// that answers, and presence that needs no hand. It returns the device's id —
// every question about what the device holds is asked of that id.
async function virtualAuthenticator(page: Page) {
  const cdp: CDPSession = await page.context().newCDPSession(page);
  // The domain is switched on for this session before a device can be added to
  // it; the browser refuses the second call by naming the first.
  await cdp.send('WebAuthn.enable');
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2', transport: 'internal',
      hasResidentKey: true, hasUserVerification: true,
      isUserVerified: true, automaticPresenceSimulation: true,
    },
  });
  return { cdp, authenticatorId };
}

// ceremony runs one leg pair in the page: ask the server, prompt the platform,
// answer. It reports both legs' statuses, which is all a case needs — whether a
// factor was enrolled, or the door refused before the platform was ever asked.
async function ceremony(page: Page, begin: string, verify: string, extra: Record<string, unknown> = {}, options: { otherCeremony?: boolean } = {}) {
  return page.evaluate(async ({ begin, verify, extra, otherCeremony }) => {
    const toBytes = (value: string) => {
      const padded = value.replace(/-/g, '+').replace(/_/g, '/');
      const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
      return Uint8Array.from(raw, c => c.charCodeAt(0));
    };
    const ask = async (url: string, body?: unknown) => {
      const response = await fetch(url, {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.json().catch(() => ({})) };
    };
    const begun = await ask(begin);
    if (begun.status >= 400) return { began: begun.status, answered: 0 };
    // The server's options are the standard's own bundle: the creation or
    // request options sit under publicKey, and only its byte members are turned
    // into buffers on the way to the platform. The relying-party id, the
    // timeout and the allow list stay exactly as the server wrote them.
    const offered = typeof begun.body.options === 'string'
      ? JSON.parse(begun.body.options) : begun.body.options ?? {};
    const publicKey = { ...(offered.publicKey ?? offered) };
    if (typeof publicKey.challenge === 'string') publicKey.challenge = toBytes(publicKey.challenge);
    if (publicKey.user && typeof publicKey.user.id === 'string') {
      publicKey.user = { ...publicKey.user, id: toBytes(publicKey.user.id) };
    }
    if (Array.isArray(publicKey.allowCredentials)) {
      publicKey.allowCredentials = publicKey.allowCredentials.map(c => ({ ...c, id: toBytes(c.id) }));
    }
    const created = await navigator.credentials.create({ publicKey });
    if (!created) return { began: begun.status, answered: 500 };
    const wire = (created as unknown as { toJSON?: () => { rawId?: string; response?: unknown } }).toJSON
      ? (created as unknown as { toJSON: () => { rawId?: string; response?: unknown } }).toJSON()
      : created;
    const answered = await ask(verify, {
      ceremony: otherCeremony ? '00000000-0000-4000-8000-000000000000' : begun.body.ceremony,
      response: { id: created.id, rawId: wire.rawId, response: wire.response, type: 'public-key' },
      ...extra,
    });
    return { began: begun.status, answered: answered.status };
  }, { begin, verify, extra, otherCeremony: options.otherCeremony === true });
}

// assertion runs one sign-in leg pair in the page: ask the server for a prompt,
// prompt the platform with what it sent, hand the answer back. Unlike ceremony
// above, the prompt here is one the platform *answers* — navigator.credentials.get
// over the resident credential — which is what a passkey that already exists does.
async function assertion(page: Page, begin: string, verify: string, beginBody: Record<string, unknown> = {}) {
  return page.evaluate(async ({ begin, verify, beginBody }) => {
    const toBytes = (value: string) => {
      const padded = value.replace(/-/g, '+').replace(/_/g, '/');
      const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
      return Uint8Array.from(raw, c => c.charCodeAt(0));
    };
    const ask = async (url: string, body?: unknown) => {
      const response = await fetch(url, {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.json().catch(() => ({})) };
    };
    const begun = await ask(begin, beginBody);
    if (begun.status >= 400) return { began: begun.status, answered: 0, said: `${begun.body?.detail ?? ''}` };
    const offered = typeof begun.body.options === 'string'
      ? JSON.parse(begun.body.options) : begun.body.options ?? {};
    const publicKey = { ...(offered.publicKey ?? offered) };
    if (typeof publicKey.challenge === 'string') publicKey.challenge = toBytes(publicKey.challenge);
    if (Array.isArray(publicKey.allowCredentials)) {
      publicKey.allowCredentials = publicKey.allowCredentials.map(c => ({ ...c, id: toBytes(c.id) }));
    }
    const asked = await navigator.credentials.get({ publicKey });
    if (!asked) return { began: begun.status, answered: 0, said: 'the platform answered nothing' };
    const wire = (asked as unknown as { toJSON?: () => { rawId?: string; response?: unknown } }).toJSON
      ? (asked as unknown as { toJSON: () => { rawId?: string; response?: unknown } }).toJSON()
      : asked;
    const answered = await ask(verify, {
      ceremony: begun.body.ceremony,
      response: { id: asked.id, rawId: wire.rawId, response: wire.response, type: 'public-key' },
    });
    return { began: begun.status, answered: answered.status, said: `${answered.body?.detail ?? ''}` };
  }, { begin, verify, beginBody });
}

async function signIn(page: Page) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
}

test('the enrolment leg asks this host, and refuses an answer that is not this ceremony', async ({ page }) => {
  await virtualAuthenticator(page);
  await signIn(page);
  const begun = await page.request.post(beginEnrolment, { data: {} });
  expect(begun.status()).toBe(200);
  const offered = (await begun.json()).options.publicKey;
  // The premise of the whole factor, read off the options the server sent: the
  // relying party is the host the request arrived at, and not something a page
  // can choose. A passkey made here is scoped to here when the device writes it.
  expect(offered.rp.id).toBe('localhost');
  expect(offered.challenge).toBeTruthy();
  expect(offered.user.id).toBeTruthy();
  // The browser half runs, and the answer is real; the ceremony id it is posted
  // against is not this one, so the module refuses it and no factor appears.
  const answered = await ceremony(page, beginEnrolment, finishEnrolment,
    { name: 'Not this ceremony' }, { otherCeremony: true });
  expect(answered.began).toBe(200);
  // A ceremony this server never began is the same answer as one it has already
  // spent: the module refuses without saying which, and the route draws it as a
  // 404 rather than a 401, because there is no prompt here to answer.
  expect(answered.answered).toBe(404);
  const listed = await (await page.request.get('/api/v1/auth/factors')).json();
  const kinds = ((listed?.items ?? listed) as { kind: string }[]).map(factor => factor.kind);
  expect(kinds).not.toContain('passkey');
});

test('the device keeps the passkey the browser made for the host that asked, and the client refuses anybody else', async ({ page }) => {
  const { cdp, authenticatorId } = await virtualAuthenticator(page);
  // Enrolment is a signed-in leg, so the page signs in first — and no factor is
  // written by anything below, which matters: this fixture's one account is the
  // one every other spec in the suite signs in with, and a real enrolment would
  // put a second factor in front of all of them.
  await signIn(page);
  const begun = await page.request.post(beginEnrolment, { data: {} });
  expect(begun.status()).toBe(200);
  const options = (await begun.json()).options;
  // The browser makes the credential — a real key, over the server's real
  // challenge — and nothing is posted back. The account gains no factor; the
  // device gains one thing, made for one host.
  const made = await page.evaluate(async (options) => {
    const toBytes = (value: string) => {
      const padded = value.replace(/-/g, '+').replace(/_/g, '/');
      const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
      return Uint8Array.from(raw, c => c.charCodeAt(0));
    };
    const publicKey = { ...options.publicKey };
    publicKey.challenge = toBytes(publicKey.challenge);
    publicKey.user = { ...publicKey.user, id: toBytes(publicKey.user.id) };
    const created = await navigator.credentials.create({ publicKey });
    return created ? created.id : 'nothing';
  }, options);
  expect(made).not.toBe('nothing');
  const stored = await cdp.send('WebAuthn.getCredentials', { authenticatorId });
  expect(stored.credentials.map(credential => credential.rpId)).toEqual(['localhost']);

  // Asked for another host's relying party, the browser refuses, and no request
  // leaves the page. This is the brief's cross-host refusal, and it is the
  // client that performs it: a credential made on one tenant's host is not a
  // credential on another's, whoever is holding the browser. The name the
  // refusal arrives under is SecurityError — the page's own origin is not inside
  // the relying party it was asked to sign for, which the client sees before it
  // ever looks at the credentials it holds. NotAllowedError, the other name this
  // can take, is what an rpId the origin *could* claim but no credential matches
  // answers with; both are refusals, and neither of them is a signature.
  let requests = 0;
  page.on('request', request => { if (request.url().includes('/passkey/')) requests++; });
  const refusal = await page.evaluate(async () => {
    const challenge = new Uint8Array(32);
    crypto.getRandomValues(challenge);
    try {
      const assertion = await navigator.credentials.get({
        publicKey: { challenge, rpId: 'tenantb.localhost', allowCredentials: [], userVerification: 'required' },
      });
      return assertion ? 'signed' : 'nothing';
    } catch (error) {
      return (error as Error).name;
    }
  });
  expect(['SecurityError', 'NotAllowedError']).toContain(refusal);
  expect(requests).toBe(0);
});

test('a passkey the browser enrolled answers its owner\u2019s password and opens the session', async ({ page, browser }) => {
  // The brief's journey, in the order the person does it: enrol, sign out, sign
  // in again with the password refused as half a sign-in, the passkey answering
  // the other half, and the application served to the session that opened.
  //
  // It brings its own person. The fixture's one account cannot carry this: the
  // moment a factor exists, the module refuses a bare password for it — which is
  // the second-factor rule, not a bug — and every other spec in this suite signs
  // in with that password, and rule 8 refuses to withdraw the last factor, so an
  // enrolment there could not be undone by the test that made it. This person is
  // created through the routes the tenant's own administrator uses, holds no
  // administration, and spends nothing that belongs to anybody else.
  const personEmail = 'passkey-journey@e2e.test';
  const personPassword = 'a-passkey-journey-password';
  await signIn(page);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: personEmail, displayName: 'Passkey journey', roles: ['member'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const created = (await invited.json()).id as string;
  expect(created).toBeTruthy();
  expect((await page.request.post(`/api/v1/user/users/${created}/set-password`, {
    data: { password: personPassword },
  })).status()).toBe(200);

  // The person, in a browser of their own, with a device of their own.
  const context = await browser.newContext();
  const person = await context.newPage();
  const { cdp, authenticatorId } = await virtualAuthenticator(person);

  // Half one: while no passkey exists, the password is the whole sign-in.
  await person.goto('/app/admin/login');
  await person.getByRole('textbox', { name: 'Email', exact: true }).fill(personEmail);
  await person.getByLabel('Password').fill(personPassword);
  await person.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(person).toHaveURL(/\/app$/);

  // Half two: the person enrols by clicking, on the screen the shell draws, in
  // the controller the shell ships. Neither the options nor the answer below is
  // synthesised: the page's own fetch legs run against the real routes.
  await person.goto('/app/auth/sessions');
  await person.getByLabel('What this device is called').fill('Playwright laptop');
  await person.getByRole('button', { name: 'Add a passkey' }).click();
  await expect(person.locator('[data-auth-message]')).toBeVisible();
  const listed = await (await person.request.get('/api/v1/auth/factors')).json();
  const kinds = ((listed?.items ?? listed) as { kind: string }[]).map(factor => factor.kind);
  expect(kinds).toContain('passkey');
  const stored = await cdp.send('WebAuthn.getCredentials', { authenticatorId });
  expect(stored.credentials.map(credential => credential.rpId)).toEqual(['localhost']);

  // Half three: signed out, the same credentials now arrive back as a refusal,
  // because the person holds a factor and half a sign-in is not a sign-in.
  await person.locator('[data-sign-out]').click();
  await expect(person).toHaveURL(/\/app\/admin\/login$/);
  await person.getByRole('textbox', { name: 'Email', exact: true }).fill(personEmail);
  await person.getByLabel('Password').fill(personPassword);
  await person.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(person).toHaveURL(/\/app\/admin\/login$/);
  await expect(person.locator('[data-login-error]')).toContainText('second factor');

  // Half four: the passkey answers the half the password opened, and the session
  // it opens is the one the application serves. The ceremony was begun at the
  // challenge door, so it is spent there — the same answer cannot be carried to
  // the usernameless door, which is the server's record and not the caller's.
  const answered = await assertion(person, '/api/v1/auth/challenge/passkey/begin',
    '/api/v1/auth/challenge/passkey/verify', { email: personEmail });
  expect(answered.said).toBe('');
  expect(answered.began).toBe(200);
  expect(answered.answered).toBe(200);
  expect((await person.request.get('/api/v1/auth/me')).status()).toBe(200);
  await person.goto('/app');
  await expect(person).toHaveURL(/\/app$/);

  // Half five: the door a tenant may open — and the one this fixture's tenant
  // keeps shut. The same passkey, on the same device, with no password offered
  // first: the usernameless ceremony the shell's own button runs. The
  // administrator opens it over the route that guards it and closes it again
  // here, so the tenant is left as this journey found it and the case below,
  // which pins the refusal, still describes the tenant every installation has.
  await person.locator('[data-sign-out]').click();
  await expect(person).toHaveURL(/\/app\/admin\/login$/);
  const opened = await page.request.post('/api/v1/auth/settings/passkey-sign-in', { data: { enabled: true } });
  expect(opened.status(), await opened.text()).toBe(200);
  expect((await opened.json()).enabled).toBe(true);
  await person.getByRole('button', { name: 'Continue with a passkey' }).click();
  await expect(person).toHaveURL(/\/app$/);
  expect((await person.request.get('/api/v1/auth/me')).status()).toBe(200);
  const shut = await page.request.post('/api/v1/auth/settings/passkey-sign-in', { data: { enabled: false } });
  expect(shut.status(), await shut.text()).toBe(200);
  expect((await shut.json()).enabled).toBe(false);

  await context.close();
});

test('a tenant that has not opened the usernameless door is refused with the reason', async ({ page }) => {
  await virtualAuthenticator(page);
  await signIn(page);
  // The tenant this case is about is the one every installation starts with: no
  // row, door shut. It is said rather than assumed, because the journey above
  // opens the door and closes it, and a case that depends on another case's
  // tidiness is a case that fails for the wrong reason.
  const shut = await page.request.post('/api/v1/auth/settings/passkey-sign-in', { data: { enabled: false } });
  expect(shut.status(), await shut.text()).toBe(200);
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  const begun = await page.request.post(beginSignIn);
  expect(begun.status()).toBe(403);
  const refusal = await begun.json();
  expect(`${refusal?.detail ?? refusal?.title ?? ''}`).not.toBe('');
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(403);
});
