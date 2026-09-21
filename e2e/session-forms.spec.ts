import { expect, test, type Page } from '@playwright/test';

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const endpoint = (kind: string) => `/api/v1/auth/${['forgot', 'reset'].includes(kind) ? 'password/' : ''}${kind}`;

// Synthetic documents isolate the shipped controller contract. Their API
// responses below are injected; they do not establish delivered account email.
async function form(page: Page, kind: string, query = '', next = '/app/admin/login') {
  await page.route('**/__auth-form?*', route => route.fulfill({
    contentType: 'text/html',
    headers: { 'Referrer-Policy': 'no-referrer', 'Cache-Control': 'no-store' },
    body: `<!doctype html><html lang="en" data-signin="/app/admin/login"><head>
      <link rel="stylesheet" href="/app/admin/assets/app.css">
      <script src="/app/admin/assets/js/session.js" defer></script></head><body><main>
      <h1>Account form</h1><form data-auth-form="${kind}" action="${endpoint(kind)}" data-next="${next}">
      <div role="alert" data-auth-error hidden></div>
      <div role="status" data-auth-message hidden>Check your inbox for the account link.</div>
      <label>Email<input name="email" type="email" required autocomplete="email"></label>
      <label>Name<input name="displayName" autocomplete="name"></label>
      <label>Password<input name="new" type="password" autocomplete="new-password"></label>
      <button type="submit">Continue</button></form></main></body></html>`,
  }));
  await page.goto(`/__auth-form?${query}`);
  await page.getByLabel('Email', { exact: true }).fill('person@example.test');
}

test('login retains the guarded query and reports a real credential refusal accessibly', async ({ page }) => {
  await page.goto('/app/task/tasks?limit=7&offset=0');
  expect(new URL(page.url()).searchParams.get('next')).toBe('/app/task/tasks?limit=7&offset=0');
  const error = page.locator('[data-login-error]');
  await expect(error).toBeHidden();
  await expect(page.locator('[data-session-error]')).toBeHidden();
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill('incorrect credentials');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(error).toBeVisible();
  await expect(error).toBeFocused();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeEnabled();
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app\/task\/tasks\?limit=7&offset=0$/);
});

for (const failure of ['http', 'network']) test(`logout ${failure} failure retains the page and allows an explicit retry`, async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
  let attempts = 0;
  await page.route('**/api/v1/auth/logout', async route => {
    attempts++;
    if (failure === 'network') await route.abort('failed');
    else await route.fulfill({ status: 503, body: 'Unavailable' });
  });
  await page.locator('[data-sign-out]').click();
  const error = page.locator('[data-session-error]');
  await expect(error).toContainText('Sign-out could not be confirmed');
  await expect(error).toBeFocused();
  await expect(page).toHaveURL(/\/app$/);
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(200);
  expect(attempts).toBe(1);
  await page.unroute('**/api/v1/auth/logout');
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(403);
});

for (const kind of ['register', 'forgot']) test(`${kind} sends its declared fields once and announces the email acknowledgment`, async ({ page }) => {
  const bodies: unknown[] = [];
  let release!: () => void;
  const wait = new Promise<void>(resolve => { release = resolve; });
  await page.route(`**${endpoint(kind)}`, async route => {
    bodies.push(route.request().postDataJSON());
    await wait;
    await route.fulfill({ status: 202, contentType: 'application/json', body: '{}' });
  });
  await form(page, kind);
  await page.getByLabel('Name', { exact: true }).fill('A person');
  await page.getByLabel('Password').fill('never send an unrelated password');
  await page.getByRole('button', { name: 'Continue' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Continue' })).toBeDisabled();
  await page.locator('form').evaluate(node => {
    node.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await expect.poll(() => bodies.length).toBe(1);
  release();
  await expect(page.getByRole('status')).toHaveText('Check your inbox for the account link.');
  await expect(page.getByRole('status')).toBeFocused();
  await expect(page.getByRole('alert')).toBeHidden();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeEnabled();
  expect(bodies).toEqual([kind === 'register'
    ? { email: 'person@example.test', displayName: 'A person' }
    : { email: 'person@example.test' }]);
});

test('reset keeps the token out of URL, DOM and storage, retains a refusal, then consumes it on success', async ({ page }) => {
  const token = 'disposable-controller-token';
  const bodies: unknown[] = [];
  await page.route('**/api/v1/auth/password/reset', async route => {
    bodies.push(route.request().postDataJSON());
    await route.fulfill({
      status: bodies.length === 1 ? 422 : 200,
      contentType: 'application/json',
      body: bodies.length === 1 ? '{"detail":"Choose a different password."}' : '{}',
    });
  });
  await form(page, 'reset', `token=${token}&locale=pt`);
  expect(new URL(page.url()).search).toBe('?locale=pt');
  expect(await page.content()).not.toContain(token);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(token);
  await page.getByLabel('Password').fill('a sufficiently long password');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('alert')).toHaveText('Choose a different password.');
  await expect(page.getByRole('alert')).toBeFocused();
  await expect(page.getByLabel('Password')).toHaveValue('a sufficiently long password');
  expect(bodies).toHaveLength(1);
  await page.getByLabel('Password').fill('another sufficiently long password');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  expect(bodies).toEqual([
    { token, new: 'a sufficiently long password' },
    { token, new: 'another sufficiently long password' },
  ]);
});

for (const query of ['', 'token=one&token=two']) test(`reset refuses missing or ambiguous token: ${query || 'missing'}`, async ({ page }) => {
  let writes = 0;
  await page.route('**/api/v1/auth/password/reset', async route => { writes++; await route.abort(); });
  await form(page, 'reset', query);
  await page.getByLabel('Password').fill('a sufficiently long password');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('alert')).toContainText('Request a new link');
  await expect(page.getByRole('alert')).toBeFocused();
  expect(new URL(page.url()).searchParams.has('token')).toBe(false);
  expect(writes).toBe(0);
});

test('malformed refusals and a lost response are visible without replaying the request', async ({ page }) => {
  let attempts = 0;
  await page.route('**/api/v1/auth/password/forgot', async route => {
    attempts++;
    if (attempts === 1) await route.fulfill({ status: 503, body: '<p>Unavailable</p>' });
    else await route.abort('failed');
  });
  await form(page, 'forgot');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('alert')).toContainText('could not be completed');
  expect(attempts).toBe(1);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('alert')).toContainText('outcome is unknown');
  await expect(page.getByLabel('Email', { exact: true })).toHaveValue('person@example.test');
  expect(attempts).toBe(2);
});

test('auth forms refuse offsite actions and offsite return destinations', async ({ page }) => {
  let external = 0;
  await page.route('https://elsewhere.invalid/**', async route => { external++; await route.abort(); });
  await form(page, 'login', '', '/\\elsewhere.invalid');
  await page.locator('form').evaluate(node => node.setAttribute('action', 'https://elsewhere.invalid/login'));
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('alert')).toContainText('outcome is unknown');
  expect(external).toBe(0);
  await page.locator('form').evaluate(node => node.setAttribute('action', '/api/v1/auth/login'));
  await page.route('**/api/v1/auth/login', route => route.fulfill({ status: 200, body: '{}' }));
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe('/');
  expect(external).toBe(0);
});
