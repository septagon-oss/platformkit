import { randomUUID } from 'node:crypto';
import { expect, test, type Locator, type Page } from '@playwright/test';

// Synthetic documents isolate the published controller contract. The script is
// served by the real fixture application; intercepted JSON responses do not
// establish backend registration, token consumption or mail-delivery behavior.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1';
test.use({ trace: 'off', screenshot: 'off', video: 'off' });

const root = process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099';
const success = 'Consulte a sua caixa de entrada.';
const endpoints: Record<string, string> = {
  register: '/api/v1/auth/register',
  'register-password': '/api/v1/auth/register',
  'verify-email': '/api/v1/auth/verify-email',
  'resend-verification': '/api/v1/auth/resend-verification',
};

function attribute(value: string) {
  return value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;');
}

async function specimen(page: Page, kind: string, options: {
  query?: string; next?: string; signin?: string; action?: string;
} = {}) {
  const name = kind === 'register' || kind === 'register-password';
  const password = kind === 'register-password';
  const fields = kind === 'verify-email' ? '' : `
    <label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required>
    ${name ? '<label for="name">Full name</label><input id="name" name="displayName" autocomplete="name" required>' : ''}
    ${password ? `<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="new-password" required>
      <label for="confirmation">Confirm password</label><input id="confirmation" name="confirmation" type="password" autocomplete="new-password" required>
      <label><input name="termsAccepted" type="checkbox" required>I accept the terms</label>` : ''}`;
  await page.route('**/__email-forms**', route => route.fulfill({
    contentType: 'text/html', headers: { 'Cache-Control': 'no-store', 'Referrer-Policy': 'no-referrer' },
    body: `<!doctype html><html lang="pt-PT" data-signin="${attribute(options.signin ?? '/__email-complete?lang=pt')}">
      <head><title>Account controller specimen</title><script defer src="/app/admin/assets/js/session.js"></script></head>
      <body><main><h1>Account controller specimen</h1>
        <form method="post" action="${attribute(options.action ?? endpoints[kind])}"
          data-auth-form="${kind}" data-next="${attribute(options.next ?? '/__email-complete?lang=pt')}" aria-label="Account">
          <p role="alert" data-auth-error hidden></p><p role="status" data-auth-message hidden>${success}</p>
          ${fields}<button type="submit">Continue</button><button type="submit" disabled>Unavailable</button>
        </form><a href="/__email-complete?recovery=1">Request a new link</a></main></body></html>`,
  }));
  await page.route('**/__email-complete**', route => route.fulfill({
    contentType: 'text/html', body: '<!doctype html><title>Continue</title><h1>Continue safely</h1>',
  }));
  // A browser navigation error must not print an emailed bearer URL.
  try { await page.goto('/__email-forms' + (options.query ?? '?lang=pt')); }
  catch { throw new Error('Could not open the synthetic account document'); }
}

async function fillSecret(input: Locator, value: string) {
  try { await input.fill(value); }
  catch { throw new Error('Could not fill the synthetic password control'); }
}

async function signup(page: Page, password: string) {
  await page.getByLabel('Email', { exact: true }).fill('fixture@example.test');
  await page.getByLabel('Full name').fill('Fixture User');
  await fillSecret(page.getByLabel('Password', { exact: true }), password);
  await fillSecret(page.getByLabel('Confirm password'), password);
  await page.getByLabel('I accept the terms').check();
}

async function submit(page: Page) {
  await page.getByRole('button', { name: 'Continue', exact: true }).focus();
  await page.keyboard.press('Enter');
}

async function destination(page: Page, path: string) {
  // A failed navigation assertion must not print a still-present bearer URL.
  await expect.poll(() => page.url() === new URL(path, root).href).toBe(true);
}

test('password signup submits one JSON request, clears credentials and announces the composed acknowledgment', async ({ page }) => {
  const password = randomUUID();
  let requests = 0, body: Record<string, unknown> = {};
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.addInitScript(() => {
    const original = window.fetch.bind(window);
    window.fetch = (input, init) => {
      document.documentElement.dataset.fetchOptions = JSON.stringify({
        method: init?.method, credentials: init?.credentials, redirect: init?.redirect, headers: init?.headers,
      });
      return original(input, init);
    };
  });
  await page.route('**/api/v1/auth/register', async route => {
    requests++;
    body = route.request().postDataJSON();
    await gate;
    await route.fulfill({ status: 202, contentType: 'application/json', body: '{}' });
  });
  await specimen(page, 'register-password');
  await signup(page, password);
  // A composed visibility toggle must not make successful signup retain a secret.
  await page.getByLabel('Confirm password').evaluate((input: HTMLInputElement) => { input.type = 'text'; });
  const before = page.url();
  try {
    await submit(page);
    await expect.poll(() => requests).toBe(1);
    await expect(page.getByRole('form')).toHaveAttribute('aria-busy', 'true');
    await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeDisabled();
    await expect(page.locator('[data-auth-error]')).toBeHidden();
    await expect(page.locator('[data-auth-message]')).toBeHidden();
    await page.getByRole('form').dispatchEvent('submit');
    expect(requests).toBe(1);
  } finally { release(); }
  const acknowledgment = page.locator('[data-auth-message]');
  await expect(acknowledgment).toHaveText(success);
  await expect(acknowledgment).toBeFocused();
  expect(await page.getByLabel('Password', { exact: true }).inputValue() === '').toBe(true);
  expect(await page.getByLabel('Confirm password').inputValue() === '').toBe(true);
  await expect(page.getByLabel('Email', { exact: true })).toHaveValue('fixture@example.test');
  await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Unavailable' })).toBeDisabled();
  expect(await page.getByRole('form').getAttribute('aria-busy')).toBeNull();
  expect(page.url()).toBe(before);
  expect(Object.keys(body).sort()).toEqual(['confirmation', 'displayName', 'email', 'password', 'termsAccepted']);
  expect(body.password === password && body.confirmation === password).toBe(true);
  expect(body.termsAccepted).toBe(true);
  const options = JSON.parse((await page.locator('html').getAttribute('data-fetch-options'))!);
  expect(options).toEqual({ method: 'POST', credentials: 'same-origin', redirect: 'error', headers: { 'Content-Type': 'application/json' } });
});

test('password signup retains failed input and sends unchecked consent as false on an explicit retry', async ({ page }) => {
  const password = randomUUID();
  let requests = 0, consent: unknown;
  await page.route('**/api/v1/auth/register', async route => {
    requests++;
    consent = route.request().postDataJSON().termsAccepted;
    if (requests === 1) await route.abort('failed');
    else await route.fulfill({ status: 422, contentType: 'application/problem+json', body: '{"detail":"Accept the terms before requesting an account."}' });
  });
  await specimen(page, 'register-password');
  await signup(page, password);
  await submit(page);
  const error = page.locator('[data-auth-error]');
  await expect(error).toBeFocused();
  await expect(error).toContainText('outcome is unknown');
  expect(await page.getByLabel('Password', { exact: true }).inputValue() === password).toBe(true);
  expect(await page.getByLabel('Confirm password').inputValue() === password).toBe(true);
  expect(requests).toBe(1);
  await page.getByLabel('I accept the terms').uncheck();
  // A modified client cannot make an unchecked checkbox serialize as consent.
  await page.getByLabel('I accept the terms').evaluate((input: HTMLInputElement) => { input.required = false; });
  await submit(page);
  await expect(error).toHaveText('Accept the terms before requesting an account.');
  await expect(error).toBeFocused();
  expect(consent).toBe(false);
  expect(requests).toBe(2);
  expect(await page.getByLabel('Password', { exact: true }).inputValue() === password).toBe(true);
});

for (const kind of ['register', 'resend-verification']) {
  test(`${kind} keeps its email-only contract and permits a deliberate retry after refusal`, async ({ page }) => {
    let requests = 0, fields: string[] = [];
    await page.route('**' + endpoints[kind], async route => {
      requests++;
      fields = Object.keys(route.request().postDataJSON()).sort();
      await route.fulfill({ status: requests === 1 ? 429 : 202, contentType: 'application/json',
        body: requests === 1 ? '{"detail":"Wait before requesting another email."}' : '{}' });
    });
    await specimen(page, kind);
    await page.getByLabel('Email', { exact: true }).fill('fixture@example.test');
    if (kind === 'register') await page.getByLabel('Full name').fill('Fixture User');
    const before = page.url();
    await submit(page);
    await expect(page.locator('[data-auth-error]')).toBeFocused();
    await expect(page.getByLabel('Email', { exact: true })).toHaveValue('fixture@example.test');
    expect(requests).toBe(1);
    await submit(page);
    await expect(page.locator('[data-auth-message]')).toHaveText(success);
    await expect(page.locator('[data-auth-message]')).toBeFocused();
    await expect(page.locator('[data-auth-error]')).toBeHidden();
    expect(fields).toEqual(kind === 'register' ? ['displayName', 'email'] : ['email']);
    expect(requests).toBe(2);
    expect(page.url()).toBe(before);
  });
}

test('verification GET is read-only, scrubs the bearer and waits for keyboard confirmation', async ({ page }) => {
  const token = randomUUID();
  const next = '/__email-complete?lang=pt&next=%2Fcollect%3Fformat%3Dpocket';
  let requests = 0, correctBody = false, scriptReferrer: string | undefined;
  page.on('request', request => {
    if (new URL(request.url()).pathname === '/app/admin/assets/js/session.js') scriptReferrer = request.headers().referer;
  });
  await page.route('**/api/v1/auth/verify-email', async route => {
    requests++;
    const body = route.request().postDataJSON();
    correctBody = Object.keys(body).length === 1 && body.token === token;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });
  await specimen(page, 'verify-email', { query: '?token=' + token + '&lang=pt', next });
  expect(requests).toBe(0);
  expect(new URL(page.url()).searchParams.has('token')).toBe(false);
  expect(new URL(page.url()).searchParams.get('lang')).toBe('pt');
  expect((await page.content()).includes(token)).toBe(false);
  await expect(page.locator('input[name=token]')).toHaveCount(0);
  expect(scriptReferrer === undefined).toBe(true);
  await expect(page.locator('[data-auth-error]')).toBeHidden();
  await submit(page);
  await destination(page, next);
  expect(requests).toBe(1);
  expect(correctBody).toBe(true);
});

test('missing and ambiguous verification links scrub tokens and focus a recovery message without a POST', async ({ page }) => {
  let writes = 0;
  await page.route('**/api/v1/auth/verify-email', route => { writes++; return route.fulfill({ status: 500 }); });
  for (const query of ['?lang=pt', '?token=&lang=pt', '?token=one&token=two&lang=pt']) {
    await specimen(page, 'verify-email', { query });
    expect(new URL(page.url()).searchParams.has('token')).toBe(false);
    await submit(page);
    const error = page.locator('[data-auth-error]');
    await expect(error).toContainText('verification link is missing or invalid');
    await expect(error).toBeFocused();
    await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeEnabled();
    await page.getByRole('link', { name: 'Request a new link' }).focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: 'Continue safely' })).toBeVisible();
  }
  expect(writes).toBe(0);
});

test('verification retains its private token through refused and uncertain outcomes without automatic replay', async ({ page }) => {
  const token = randomUUID();
  let requests = 0, correctBodies = true;
  await page.route('**/api/v1/auth/verify-email', async route => {
    requests++;
    correctBodies &&= route.request().postDataJSON().token === token;
    if (requests === 2) await route.abort('failed');
    else await route.fulfill({ status: requests === 1 ? 503 : 200, contentType: 'application/json',
      body: requests === 1 ? '{"detail":"Verification temporarily unavailable."}' : '{}' });
  });
  await specimen(page, 'verify-email', { query: '?token=' + token });
  await submit(page);
  const error = page.locator('[data-auth-error]');
  await expect(error).toHaveText('Verification temporarily unavailable.');
  await expect(error).toBeFocused();
  expect(requests).toBe(1);
  await submit(page);
  await expect(error).toContainText('outcome is unknown');
  await expect(error).toBeFocused();
  expect(requests).toBe(2);
  expect((await page.content()).includes(token)).toBe(false);
  await submit(page);
  await destination(page, '/__email-complete?lang=pt');
  expect(requests).toBe(3);
  expect(correctBodies).toBe(true);
});

test('verification refuses nonlocal actions and response redirects, and falls back from nonlocal continuations', async ({ page }) => {
  const token = randomUUID();
  let external = 0, posts = 0, redirects = true;
  await page.route('https://other-tenant.invalid/**', route => { external++; return route.abort(); });
  await page.route('**/api/v1/auth/verify-email', route => {
    posts++;
    return route.fulfill(redirects
      ? { status: 303, headers: { Location: 'https://other-tenant.invalid/receive' } }
      : { status: 200, contentType: 'application/json', body: '{}' });
  });
  await specimen(page, 'verify-email', { query: '?token=' + token, action: 'https://other-tenant.invalid/verify' });
  await submit(page);
  await expect(page.locator('[data-auth-error]')).toBeFocused();
  expect(posts).toBe(0);
  expect(external).toBe(0);
  await specimen(page, 'verify-email', { query: '?token=' + token });
  await submit(page);
  await expect(page.locator('[data-auth-error]')).toContainText('outcome is unknown');
  expect(posts).toBe(1);
  expect(external).toBe(0);
  redirects = false;
  for (const next of ['https://other-tenant.invalid/', '//other-tenant.invalid/', '/\\other-tenant.invalid/']) {
    await specimen(page, 'verify-email', { query: '?token=' + token, next,
      signin: '/__email-complete?lang=pt&next=%2Fcollect%3Fformat%3Dpocket' });
    await submit(page);
    await destination(page, '/__email-complete?lang=pt&next=%2Fcollect%3Fformat%3Dpocket');
  }
  expect(external).toBe(0);
});
