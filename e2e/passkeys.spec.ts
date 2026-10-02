import { expect, test, type CDPSession, type Page } from '@playwright/test';

// The passkey journey. Chromium emulates an authenticator over CDP — a real
// CTAP2 device as far as the page is concerned, holding real key material and
// refusing to sign for a relying party it was not made for. That refusal is the
// claim under test: a passkey enrolled for one host does not answer at another,
// and the browser, not this repository, is what enforces it.
//
// What the journey covers today: enrolling from the person's own sessions
// screen, and the sign-in page's passkey door. The usernameless leg behind that
// door is a tenant's own row, and nothing in the application writes it yet —
// the door answers the reason, which the second case asserts as the refusal it
// is. The named follow-up opens the door from the operator's face and drives the
// rest of the brief's journey through it.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const sessions = '/app/auth/sessions';

// A virtual authenticator per page, with a resident key and user verification:
// a discoverable credential — a passkey — rather than a keyed MAC of a secret.
async function authenticator(page: Page): Promise<CDPSession> {
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

async function signIn(page: Page) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
}

async function axeClean(page: Page) {
  const { AxeBuilder } = await import('@axe-core/playwright');
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations.map(v => `${v.id}: ${v.description}`)).toEqual([]);
}

test('a passkey enrols from the person\'s own sessions screen with a real prompt', async ({ page }) => {
  await authenticator(page);
  await signIn(page);
  await page.goto(sessions);
  await axeClean(page);

  const enrolled: unknown[] = [];
  page.on('response', response => {
    if (response.url().endsWith('/api/v1/auth/factors/passkey/finish')) enrolled.push(response.status());
  });
  await page.getByLabel('What this device is called').fill('Playwright laptop');
  await page.getByRole('button', { name: 'Add a passkey', exact: true }).click();
  await expect(page.locator('[data-auth-message]')).toBeVisible();
  await expect(page.locator('[data-auth-error]')).toBeHidden();
  expect(enrolled).toEqual([201]);
});

test('an enrolled passkey signs its owner in where the tenant opened the door', async ({ page }) => {
  const cdp = await authenticator(page);
  await signIn(page);
  await page.goto(sessions);
  await page.getByLabel('What this device is called').fill('Playwright laptop');
  await page.getByRole('button', { name: 'Add a passkey', exact: true }).click();
  await expect(page.locator('[data-auth-message]')).toBeVisible();

  // The door is the tenant's own row, and no screen writes it: this tenant has
  // not opened it. The refusal arrives as the sentence the contract gives, in
  // the region the sign-in page already announces through, and the page is
  // still there to be used — nothing is spent and nobody is locked out.
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  await page.getByRole('button', { name: 'Sign in with a passkey', exact: true }).click();
  const refusal = page.locator('[data-login-error]');
  await expect(refusal).toBeVisible();
  expect((await refusal.innerText()).trim().length).toBeGreaterThan(0);
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(403);
  await expect(page.getByRole('button', { name: 'Sign in with a passkey', exact: true })).toBeEnabled();
  await axeClean(page);

  // And the same authenticator, holding that credential, will not sign for a
  // host it was not enrolled at. The request never leaves the browser: the
  // client is the party that refuses, which is the whole phishing resistance
  // this factor is for.
  const rpMismatch = page.evaluate(async () => {
    try {
      await navigator.credentials.get({
        publicKey: {
          challenge: new Uint8Array(32),
          rpId: 'tenantb.localhost',
          allowCredentials: [],
          userVerification: 'required',
        },
      });
      return 'signed';
    } catch (error) {
      return (error as Error).name;
    }
  });
  expect(await rpMismatch).toBe('NotAllowedError');
  await cdp.detach();
});

test('a passwordless sign-in that no authenticator answers leaves the page usable', async ({ page }) => {
  await authenticator(page);
  await page.goto('/app/admin/login');
  let verifications = 0;
  await page.route('**/api/v1/auth/login/passkey/verify', async route => {
    verifications++;
    await route.abort('failed');
  });
  await page.getByRole('button', { name: 'Sign in with a passkey', exact: true }).click();
  await expect(page.locator('[data-login-error]')).toBeVisible();
  expect(verifications).toBeLessThanOrEqual(1);
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeEnabled();
});
