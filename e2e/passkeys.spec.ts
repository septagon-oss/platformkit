import { expect, test, type Page } from '@playwright/test';

// The passkey journey, driven by a browser and a real (emulated) authenticator.
//
// Chromium speaks CTAP2 over CDP: WebAuthn.addVirtualAuthenticator installs a
// device the page can prompt, holding real key material, that refuses to sign
// for a relying party it was not enrolled at. That refusal is the claim here — a
// passkey made for one host does not answer at another, and it is the client,
// not this repository, that enforces it.
//
// The ceremony legs are the auth module's published routes, called from the page
// with the page's own cookie jar: the browser half (navigator.credentials) and
// the server half (the two POSTs) both run, and nothing is synthesised in
// between. What the journey does not yet do is click its way through a screen to
// do it: the screens, the operator's switch behind the usernameless door and the
// second-factor leg are named as the remaining edits in the delivery report.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const beginEnrolment = '/api/v1/auth/factors/passkey/begin';
const finishEnrolment = '/api/v1/auth/factors/passkey/finish';
const beginSignIn = '/api/v1/auth/login/passkey/begin';

async function virtualAuthenticator(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    authenticator: {
      protocol: 'ctap2', transport: 'internal',
      hasResidentKey: true, hasUserVerification: true,
      isUserVerified: true, automaticPresenceSimulation: true,
    },
  });
  return cdp;
}

// ceremony runs one leg pair in the page: ask the server, prompt the platform,
// answer. It returns the verify leg's status, which is the only fact the case
// needs — whether a factor was enrolled, or a sign-in opened.
async function run(page: Page, begin: string, verify: string, extra: Record<string, unknown> = {}) {
  return page.evaluate(async ({ begin, verify, extra }) => {
    const bytes = (buffer: ArrayBuffer) =>
      btoa(String.fromCharCode(...new Uint8Array(buffer)))
        .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    const toBytes = (value: string) => {
      const padded = value.replace(/-/g, '+').replace(/_/g, '/');
      const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
      return Uint8Array.from(raw, (c: string) => c.charCodeAt(0));
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
    const publicKey = { ...(begun.body.options ?? {}) };
    if (typeof publicKey.challenge === 'string') publicKey.challenge = toBytes(publicKey.challenge);
    if (publicKey.user && typeof publicKey.user.id === 'string') {
      publicKey.user = { ...publicKey.user, id: toBytes(publicKey.user.id) };
    }
    if (Array.isArray(publicKey.allowCredentials)) {
      publicKey.allowCredentials = publicKey.allowCredentials.map((c: any) => ({ ...c, id: toBytes(c.id) }));
    }
    const created = await navigator.credentials.create({ publicKey });
    if (!created) return { began: begun.status, answered: 500 };
    const wire = (created as any).toJSON ? (created as any).toJSON() : created;
    const answered = await ask(verify, {
      ceremony: begun.body.ceremony,
      response: { id: created.id, rawId: wire.rawId, response: wire.response, type: 'public-key' },
      ...extra,
    });
    return { began: begun.status, answered: answered.status, bytes: bytes(new Uint8Array(wire.response.attestationObject ?? new ArrayBuffer(0))) };
  }, { begin, verify, extra });
}

test.beforeEach(async ({ page }) => {
  await virtualAuthenticator(page);
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
});

test('a passkey made by the browser enrols as a factor through the real ceremony', async ({ page }) => {
  const result = await run(page, beginEnrolment, finishEnrolment, { name: 'Playwright laptop' });
  expect(result.began).toBe(200);
  expect(result.answered).toBe(201);
  // The factor is a fact about the account, and the list says so in both kinds.
  const factors = await page.request.get('/api/v1/auth/factors');
  expect(factors.status()).toBe(200);
  const listed = await factors.json();
  const kinds = ((listed?.items ?? listed) as any[]).map((f: any) => f.kind);
  expect(kinds).toContain('passkey');
});

test('the same authenticator will not sign for a host it was not enrolled at', async ({ page }) => {
  expect((await run(page, beginEnrolment, finishEnrolment, { name: 'Playwright laptop' })).answered).toBe(201);
  // The credential is resident in the emulated device, scoped to rpId
  // localhost. Asked for somebody else's relying party, the browser refuses
  // before any request leaves it: no request was routed, no challenge was
  // spent, no answer was posted.
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
  expect(refusal).toBe('NotAllowedError');
});

test('a tenant that has not opened the usernameless door is refused with the reason', async ({ page }) => {
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  const begun = await page.request.post(beginSignIn);
  expect(begun.status()).toBe(403);
  const refusal = await begun.json();
  expect(`${refusal?.detail ?? refusal?.title ?? ''}`).not.toBe('');
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(403);
});
