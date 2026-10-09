import { expect, test, type CDPSession, type Page } from '@playwright/test';

// The brief's cross-tenant refusal, walked in a browser: one person, one device,
// two tenants served at two hosts.
//
// `e2e/passkeys.spec.ts` refuses another tenant's relying party by asking the
// browser for an rpId the page's own origin cannot claim — Chromium refuses that
// before any server is asked, so it proves the client's rule and nothing about a
// second tenant. Here the page really is at the second tenant's host: the options
// come from that tenant's server, the platform is asked with them, and the answer
// — none — is the credential's own scope. On the way out the same device still
// signs the person in at the first tenant's host, which is what makes the refusal
// at the second one a fact about relying-party ids rather than a dead device.
//
// The second tenant is a second installation of this same application, at
// tenantb.localhost, on a database and a port of its own. scripts/e2e.sh says why:
// `bootstrap` refuses a second tenant where one exists, and the control plane that
// could create one answers at no host this fixture serves — `surfaces.spec.ts`
// pins that. A second installation is a stronger wall than row-level security,
// since nothing is shared to be isolated, so this case does not stand in for the
// SQL isolation, which modules/auth proves over Postgres. What only a browser can
// say is the half the brief names: the passkey a device wrote for one tenant's
// host does not answer at another's, whoever holds the browser and whatever the
// second tenant's server asks it for.

const personEmail = 'cross-tenant@e2e.test';
const personPassword = 'a-cross-tenant-journey-password';
const adminEmail = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const adminPassword = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const secondURL = process.env.PLATFORMKIT_E2E_SECOND_URL ?? '';
const secondEmail = process.env.PLATFORMKIT_E2E_SECOND_EMAIL ?? '';
const secondPassword = process.env.PLATFORMKIT_E2E_SECOND_PASSWORD ?? '';
const secondHost = new URL(secondURL).host;
const secondOriginHost = new URL(secondURL).hostname;
const secondSignIn = `${secondURL}/app/admin/login`;
const secondBegin = `${secondURL}/api/v1/auth/login/passkey/begin`;
const secondVerify = `${secondURL}/api/v1/auth/login/passkey/verify`;

// Chromium maps *.localhost to the loopback address itself; the rule states the
// mapping rather than leaving the journey at the mercy of a host's resolver. It is a
// Chromium argument, so it reaches the browser and nothing else — see askSecond for
// what that costs this file.
test.use({ launchOptions: { args: ['--host-resolver-rules=MAP *.localhost 127.0.0.1'] } });

async function virtualAuthenticator(page: Page) {
  const cdp: CDPSession = await page.context().newCDPSession(page);
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

// passwordSignIn drives the door the way a person drives it: the address, the
// password, the button. It decides nothing about the answer, because which
// addresses answer at which tenant is one of the two things this file asks.
async function passwordSignIn(page: Page, email: string, password: string) {
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}

// askSecond drives one of the second tenant's own routes, and asks it of the page
// that stands at that tenant rather than of Playwright's APIRequestContext.
//
// The reason is a difference between two network stacks on this machine, not a
// preference. `page.request` is a Node client: it asks this machine's resolver what
// `tenantb.localhost` is, and this fixture registers that name nowhere — the host
// mapping above is a Chromium argument, so Chromium is the only thing that knows it.
// The CI container's resolver does not answer for `*.localhost`, and there the claim
// died at the resolver, before the second tenant's application was asked anything
// (`apiRequestContext.get: getaddrinfo ENOTFOUND tenantb.localhost`, job 50620 of run
// 50143). A development box whose name service does answer is why the same line was
// green here: `getent ahosts tenantb.localhost` says ::1 on this machine — by NSS, not
// by /etc/hosts, since any name under `.localhost` answers the same way — while Node
// asked the same question on the runner and got ENOTFOUND. Asked from the page, the
// request leaves over the browser's resolver, the page's own origin and the cookie jar
// this journey actually signs into, which is the path every claim in this file is
// about.
//
// A page that does not stand at the tenant the address names is refused here rather
// than asked: several assertions below read `not.toBe(200)`, and the first tenant's
// refusal of a signed-out caller would satisfy one of them for the wrong reason.
async function askSecond(page: Page, url: string, options: {method?: 'GET' | 'POST'; body?: unknown} = {}) {
  return page.evaluate(async ({url, options}) => {
    if (new URL(url).origin !== location.origin) {
      throw new Error(`${url} is not a route of the tenant this page stands at (${location.origin})`);
    }
    const response = await fetch(url, {
      method: options.method ?? 'GET',
      credentials: 'include',
      headers: options.body === undefined ? undefined : {'Content-Type': 'application/json'},
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    return {
      status: response.status,
      body: (await response.json().catch(() => ({}))) as Record<string, unknown>,
    };
  }, {url, options});
}

// ceremony runs one leg pair against the host `begin` names, from a page on that
// same host, with that page's own cookie jar. It reports the relying party the
// server asked for, the name the platform refused under when it refused, and the
// status of the answer when the platform answered — which is the whole of what a
// cross-tenant case needs to see.
async function ceremony(page: Page, begin: string, verify: string, beginBody: Record<string, unknown> = {}) {
  return page.evaluate(async ({ begin, verify, beginBody }) => {
    const toBytes = (value: string) => {
      const padded = value.replace(/-/g, '+').replace(/_/g, '/');
      const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
      return Uint8Array.from(raw, c => c.charCodeAt(0));
    };
    const ask = async (url: string, body?: unknown) => {
      const response = await fetch(url, {
        method: 'POST', credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.json().catch(() => ({})) };
    };
    const begun = await ask(begin, beginBody);
    if (begun.status >= 400) {
      return { began: begun.status, rpId: '', refused: '', said: `${begun.body?.detail ?? ''}`, answered: 0 };
    }
    const offered = typeof begun.body.options === 'string'
      ? JSON.parse(begun.body.options) : begun.body.options ?? {};
    const publicKey = { ...(offered.publicKey ?? offered) };
    if (typeof publicKey.challenge === 'string') publicKey.challenge = toBytes(publicKey.challenge);
    if (Array.isArray(publicKey.allowCredentials)) {
      publicKey.allowCredentials = publicKey.allowCredentials.map(c => ({ ...c, id: toBytes(c.id) }));
    }
    const rpId = `${publicKey.rpId ?? ''}`;
    try {
      const asked = await navigator.credentials.get({ publicKey });
      if (!asked) return { began: begun.status, rpId, refused: 'nothing', said: '', answered: 0 };
      const wire = (asked as unknown as { toJSON?: () => unknown }).toJSON
        ? (asked as unknown as { toJSON: () => { rawId?: string; response?: unknown } }).toJSON()
        : asked;
      const answered = await ask(verify, {
        ceremony: begun.body.ceremony,
        response: { id: asked.id, rawId: wire.rawId, response: wire.response, type: 'public-key' },
      });
      return { began: begun.status, rpId, refused: '', said: `${answered.body?.detail ?? ''}`, answered: answered.status };
    } catch (error) {
      return { began: begun.status, rpId, refused: (error as Error).name, said: '', answered: 0 };
    }
  }, { begin, verify, beginBody });
}

test('a passkey written for one tenant\'s host does not answer at another tenant\'s host', async ({ page, browser }) => {
  test.setTimeout(90_000);
  expect(secondURL, 'scripts/e2e.sh serves a second tenant at its own host').not.toBe('');

  // The person is created through the routes the first tenant's own administrator
  // uses, and enrols by clicking on the screen the shell draws: the passkey this
  // journey carries was written by the browser, at the first tenant's host, in a
  // ceremony that tenant's server asked for.
  await page.goto('/app/admin/login');
  await passwordSignIn(page, adminEmail, adminPassword);
  await expect(page).toHaveURL(/\/app$/);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: personEmail, displayName: 'Cross tenant', roles: ['member'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const person = (await invited.json()).id as string;
  expect((await page.request.post(`/api/v1/user/users/${person}/set-password`, {
    data: { password: personPassword },
  })).status()).toBe(200);

  // One browser, one device, for everything below.
  const context = await browser.newContext();
  const device = await context.newPage();
  const { cdp, authenticatorId } = await virtualAuthenticator(device);

  await device.goto('/app/admin/login');
  await passwordSignIn(device, personEmail, personPassword);
  await expect(device).toHaveURL(/\/app$/);
  await device.goto('/app/auth/sessions');
  await device.getByLabel('What this device is called').fill('The one device');
  await device.getByRole('button', { name: 'Add a passkey' }).click();
  await expect(device.locator('[data-auth-message]')).toBeVisible();
  const madeAtHome = await cdp.send('WebAuthn.getCredentials', { authenticatorId });
  expect(madeAtHome.credentials.map(credential => credential.rpId)).toEqual(['localhost']);

  // Signed out, so the second half of this tenant's journey starts at its door.
  await device.goto('/app');
  await device.locator('[data-sign-out]').click();
  await expect(device).toHaveURL(/\/app\/admin\/login$/);

  // The second tenant, in the same browser and on the same device. The address is
  // absolute because the origin is different: a different tenant is served here,
  // at an address the first tenant is never reached at.
  await device.goto(secondSignIn);
  expect(new URL(device.url()).host, 'the second tenant is not served at its own host').toBe(secondHost);
  await expect(device.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  // WebAuthn speaks only from a potentially-trustworthy origin, and over http a
  // page gets there only as a loopback name. Said here because if it were false,
  // the refusal below would be the context's and not the credential's.
  expect(await device.evaluate(() => window.isSecureContext),
    'the second tenant is not served from a trustworthy origin').toBe(true);
  // The device crossed with the page, and it crossed carrying one thing: the key
  // it wrote for the first tenant, which the second tenant never saw.
  const carried = await cdp.send('WebAuthn.getCredentials', { authenticatorId });
  expect(carried.credentials.map(credential => credential.rpId)).toEqual(['localhost']);

  // The person from the first tenant is nobody here. The address exists in the
  // other tenant's rows, and this tenant's door says so.
  await passwordSignIn(device, personEmail, personPassword);
  await expect(device).toHaveURL(new RegExp(`/app/admin/login$`));
  expect((await askSecond(device, `${secondURL}/api/v1/auth/me`)).status).not.toBe(200);
  // And the second tenant's administrator is nobody at the first: an address that
  // exists in one tenant's rows does not answer at another's host, passkey or no
  // passkey. Asked of the first tenant's own route, in a refused answer that
  // locks nothing, because no account here carries that address.
  await page.goto('/app/admin/login');
  const stranger = await page.request.post('/api/v1/auth/login', { data: { email: secondEmail, password: secondPassword } });
  expect(stranger.status()).not.toBe(200);

  // The second tenant's own administrator opens her tenant's usernameless door,
  // over her tenant's own route — so that the refusal below is not a shut door
  // doing the work. Then she stands down, and the ceremony runs for nobody.
  await passwordSignIn(device, secondEmail, secondPassword);
  await expect(device).toHaveURL(new RegExp(`/app$`));
  expect((await askSecond(device, `${secondURL}/api/v1/auth/me`)).status).toBe(200);
  const opened = await askSecond(device, `${secondURL}/api/v1/auth/settings/passkey-sign-in`,
    {method: 'POST', body: {enabled: true}});
  expect(opened.status, JSON.stringify(opened.body)).toBe(200);
  expect(opened.body.enabled).toBe(true);
  await device.locator('[data-sign-out]').click();
  await expect(device).toHaveURL(new RegExp(`/app/admin/login$`));

  // The refusal, with the second tenant's server taking its part. Its own route
  // answers, and the relying party it asks the platform for is its own host — not
  // the host this credential was written for. The platform is what says no, and
  // nothing is posted back, because there is nothing to post.
  const refused = await ceremony(device, secondBegin, secondVerify);
  expect(refused.began, `the second tenant would not open its own door: ${refused.said}`).toBe(200);
  expect(refused.rpId, 'the second tenant asked for a relying party that is not its own host').toBe(secondOriginHost);
  expect(['NotAllowedError', 'SecurityError', 'nothing'], `the platform answered: ${refused.refused}`)
    .toContain(refused.refused);
  expect(refused.answered, 'the ceremony was answered after all').toBe(0);
  expect((await askSecond(device, `${secondURL}/api/v1/auth/me`)).status,
    'a session opened at the second tenant').not.toBe(200);
  const untouched = await cdp.send('WebAuthn.getCredentials', { authenticatorId });
  expect(untouched.credentials.map(credential => credential.rpId)).toEqual(['localhost']);

  // Back at the first tenant's host, the same device answers: the password half,
  // the refusal that says a second factor, then the passkey that finishes it. The
  // refusal above was this credential's scope, and not the device, the session or
  // the wind.
  await device.goto('/app/admin/login');
  expect(new URL(device.url()).host).toBe(new URL(process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').host);
  await passwordSignIn(device, personEmail, personPassword);
  await expect(device.locator('[data-login-error]')).toContainText('second factor');
  const home = await ceremony(device, '/api/v1/auth/challenge/passkey/begin',
    '/api/v1/auth/challenge/passkey/verify', { email: personEmail });
  expect(home.began, home.said).toBe(200);
  expect(home.refused, `the device refused its own tenant: ${home.refused}`).toBe('');
  expect(home.answered, home.said).toBe(200);
  expect((await device.request.get('/api/v1/auth/me')).status()).toBe(200);

  // The second tenant is left as this journey found it. Closing her door needs
  // her session back — it is her tenant's route and no other tenant's caller may
  // write it, which is the same rule the refusal above is an instance of.
  await device.goto(secondSignIn);
  await passwordSignIn(device, secondEmail, secondPassword);
  await expect(device).toHaveURL(new RegExp(`/app$`));
  expect((await askSecond(device, `${secondURL}/api/v1/auth/settings/passkey-sign-in`,
    {method: 'POST', body: {enabled: false}})).status).toBe(200);
  expect((await askSecond(device, `${secondURL}/api/v1/auth/login/passkey/begin`, {method: 'POST'})).status).toBe(403);
  await device.locator('[data-sign-out]').click();

  await context.close();
});
