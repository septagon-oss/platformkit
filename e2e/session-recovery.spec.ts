import { expect, test, type APIRequestContext, type Page, type Request } from '@playwright/test';

const tasks = '/admin/task/tasks';
const api = '/api/v1/task/tasks';
const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

test.beforeEach(async ({ page, request }) => {
  const target = new URL(process.env.PLATFORMKIT_E2E_URL ?? 'about:blank');
  const fixture = process.env.PLATFORMKIT_E2E_FIXTURE_DATABASE ?? '';
  if (target.protocol !== 'http:' || target.hostname !== 'localhost' ||
      target.port !== (process.env.PLATFORMKIT_E2E_PORT ?? '8099') ||
      target.username || target.password || !/^platformkit_e2e_\d+_\d+_\d+$/.test(fixture)) {
    throw new Error('Session recovery writes require the disposable local make e2e harness');
  }
  await page.addInitScript(() => {
    const writes: string[] = [];
    Object.assign(window, { recoveryStorageWrites: writes });
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      writes.push(key);
      original.call(this, key, value);
    };
  });
  // The request fixture has its own cookie jar. Its reads remain authenticated
  // when the browser's session is revoked through the ordinary logout API.
  const observer = await request.post('/api/v1/auth/login', { data: { email, password } });
  expect(observer.status()).toBe(200);
  await page.goto('/admin/login');
  await signIn(page);
});

async function signIn(page: Page, address = email, secret = password) {
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(address);
  await page.getByLabel('Password').fill(secret);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
}

async function matchingTasks(request: APIRequestContext, title: string) {
  const response = await request.get(`${api}?limit=100`);
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body.items).toHaveLength(body.total);
  return body.items.filter((row: { title: string }) => row.title === title);
}

async function openSignIn(page: Page, notice: string) {
  const link = page.locator(notice).getByRole('link', {
    name: 'Sign in (opens a new tab)', exact: true,
  });
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', /\bnoopener\b/);
  await expect(link).toHaveAttribute('rel', /\bnoreferrer\b/);
  await expect(link).toHaveAttribute('href', '/admin/login');
  // The notice follows the retained form. Tab from Save reaches the real
  // link; Enter uses the browser's ordinary new-tab behavior.
  await page.getByRole('button', { name: 'Save', exact: true }).focus();
  await page.keyboard.press('Tab');
  await expect(link).toBeFocused();
  const opened = page.context().waitForEvent('page');
  await page.keyboard.press('Enter');
  const tab = await opened;
  await expect(tab).toHaveURL(/\/admin\/login$/);
  expect(await tab.evaluate(() => window.opener === null)).toBe(true);
  return tab;
}

for (const width of [1280, 390]) {
  test(`a revoked session preserves create and edit input for explicit recovery at ${width}px`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 900 });
    let id = '';
    let previousTitle = '';
    for (const operation of ['create', 'edit']) {
      const title = `Recovery ${width} ${operation} <&> task`;
      const description = 'Keep this unsaved paragraph.\nAnd this second line.';
      const endpoint = operation === 'create' ? tasks : `${tasks}/${id}`;
      const formPath = operation === 'create' ? `${tasks}/new` : `${endpoint}/edit`;
      await page.goto(formPath);
      const field = page.getByRole('textbox', { name: 'Title', exact: true });
      await field.fill(title);
      await page.getByRole('textbox', { name: 'Description', exact: true }).fill(description);
      await page.getByRole('combobox', { name: 'Priority', exact: true }).selectOption('high');
      await field.focus();
      await field.evaluate((node: HTMLInputElement) => node.setSelectionRange(3, 12));
      const original = await field.elementHandle();
      if (!original) throw new Error('The generated title control is missing');
      // HTMX itself probes storage on document initialization. Compare the
      // complete write log across recovery; do not whitelist any storage key.
      const storageBefore = await page.evaluate(() =>
        (window as unknown as { recoveryStorageWrites: string[] }).recoveryStorageWrites.slice());
      const submissions: string[] = [];
      const record = (sent: Request) => {
        if (new URL(sent.url()).pathname === endpoint && sent.method() === 'POST') {
          submissions.push(sent.headers()['x-expected-principal'] ?? '');
        }
      };
      page.on('request', record);
      const logout = await page.request.post('/api/v1/auth/logout');
      expect(logout.status()).toBe(200);
      const refused = page.waitForResponse(response =>
        new URL(response.url()).pathname === endpoint && response.request().method() === 'POST');
      await field.press('Enter');
      const response = await refused;
      expect(response.status()).toBe(403);
      expect((await response.json()).detail).toMatch(/^AUTH_ANONYMOUS:/);
      // This is the red baseline boundary: a real auth refusal has happened,
      // before any assertions about the new recovery markup or principal data.
      const notice = page.locator('#pk-auth-anonymous');
      await expect(notice).toBeVisible();
      await expect(notice.getByRole('alert')).toContainText('Sign-in required');
      await expect(field).toBeFocused();
      expect(await field.evaluate((node: HTMLInputElement) => [node.selectionStart, node.selectionEnd])).toEqual([3, 12]);
      expect(await original.evaluate(node => node.isConnected)).toBe(true);
      await expect(field).toHaveValue(title);
      await expect(page.getByRole('textbox', { name: 'Description', exact: true })).toHaveValue(description);
      await expect(page.getByRole('combobox', { name: 'Priority', exact: true })).toHaveValue('high');
      expect(await matchingTasks(request, title)).toEqual([]);
      if (id) expect(await matchingTasks(request, previousTitle)).toHaveLength(1);

      if (operation === 'create') {
        const wrong = await openSignIn(page, '#pk-auth-anonymous');
        try {
          await wrong.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
          await wrong.getByLabel('Password').fill('not the fixture password');
          const incorrect = wrong.waitForResponse(result =>
            new URL(result.url()).pathname === '/api/v1/auth/login' && result.request().method() === 'POST');
          await wrong.getByRole('button', { name: 'Sign in', exact: true }).click();
          expect((await incorrect).status()).toBe(401);
          await expect(wrong.locator('[data-login-error]')).toBeVisible();
          await expect(wrong).toHaveURL(/\/admin\/login$/);
        } finally {
          await wrong.close();
        }
        await page.bringToFront();
        await expect(field).toHaveValue(title);
        expect(await original.evaluate(node => node.isConnected)).toBe(true);
        expect(submissions).toHaveLength(1);
      }

      const login = await openSignIn(page, '#pk-auth-anonymous');
      try {
        await signIn(login);
      } finally {
        await login.close();
      }
      await page.bringToFront();
      await expect(page).toHaveURL(new URL(formPath, page.url()).href);
      await expect(field).toHaveValue(title);
      expect(await matchingTasks(request, title)).toEqual([]);
      expect(submissions).toHaveLength(1);
      expect(await page.evaluate(() => (window as unknown as { recoveryStorageWrites: string[] }).recoveryStorageWrites)).toEqual(storageBefore);
      const principal = await page.locator('html').getAttribute('data-principal');
      expect(principal).toMatch(/^[0-9a-f-]{36}$/);
      const saved = page.waitForResponse(result =>
        new URL(result.url()).pathname === endpoint && result.request().method() === 'POST');
      await page.getByRole('button', { name: 'Save', exact: true }).click();
      expect((await saved).status()).toBe(204);
      await expect(page).toHaveURL(/\/admin\/task\/tasks\/[0-9a-f-]{36}$/);
      const persisted = await matchingTasks(request, title);
      expect(persisted).toEqual([expect.objectContaining({ title, description, priority: 'high' })]);
      if (id) expect(persisted[0].id).toBe(id);
      id = persisted[0].id;
      previousTitle = title;
      expect(submissions).toEqual([principal, principal]);
      await expect(page.locator('#pk-auth-anonymous')).toBeHidden();
      page.off('request', record);
      await original.dispose();
    }
  });
}

test('an account changed in another tab cannot receive the original form write', async ({ page, request }) => {
  const secondEmail = 'recovery-second-admin@e2e.test';
  const secondPassword = 'disposable second account password';
  const invited = await request.post('/api/v1/user/invitations', {
    data: { email: secondEmail, displayName: 'Second account', roles: ['admin'] },
  });
  expect(invited.status()).toBe(201);
  const second = await invited.json();
  expect((await request.post(`/api/v1/user/users/${second.id}/set-password`, {
    data: { password: secondPassword },
  })).status()).toBe(200);
  await page.goto(`${tasks}/new`);
  const title = 'Keep the original task author';
  const field = page.getByRole('textbox', { name: 'Title', exact: true });
  await field.fill(title);
  const principal = await page.locator('html').getAttribute('data-principal');
  const otherTab = await page.context().newPage();
  try {
    await otherTab.goto('/admin/login');
    await signIn(otherTab, secondEmail, secondPassword);
  } finally {
    await otherTab.close();
  }
  const identity = await page.request.get('/api/v1/auth/me');
  expect(identity.status()).toBe(200);
  expect((await identity.json()).userId).toBe(second.id);
  expect(second.id).not.toBe(principal);
  const rejected = page.waitForResponse(response =>
    new URL(response.url()).pathname === tasks && response.request().method() === 'POST');
  await field.press('Enter');
  const refusal = await rejected;
  expect(refusal.status()).toBe(403);
  expect(refusal.request().headers()['x-expected-principal']).toBe(principal);
  await expect(page.locator('#pk-auth-changed').getByRole('alert')).toContainText('Account changed');
  await expect(field).toHaveValue(title);
  expect(await matchingTasks(request, title)).toEqual([]);
  const login = await openSignIn(page, '#pk-auth-changed');
  try {
    await signIn(login);
  } finally {
    await login.close();
  }
  await page.bringToFront();
  expect(await matchingTasks(request, title)).toEqual([]);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/task\/tasks\/[0-9a-f-]{36}$/);
  expect(await matchingTasks(request, title)).toHaveLength(1);
});

test('a recovery notice survives validation replacement and repeated real refusals', async ({ page, request }) => {
  await page.goto(`${tasks}/new`);
  const title = 'Retain the notice across the validation swap';
  const field = page.getByRole('textbox', { name: 'Title', exact: true });
  await field.fill(title);
  const oldForm = await page.locator('#screen-form').elementHandle();
  if (!oldForm) throw new Error('The generated form is missing');
  const submit = async (status: number) => {
    const received = page.waitForResponse(response =>
      new URL(response.url()).pathname === tasks && response.request().method() === 'POST');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    expect((await received).status()).toBe(status);
  };
  expect((await page.request.post('/api/v1/auth/logout')).status()).toBe(200);
  await submit(403);
  const notice = page.locator('#pk-auth-anonymous');
  await expect(notice).toBeVisible();
  const retainedNotice = await notice.elementHandle();
  if (!retainedNotice) throw new Error('The recovery notice is missing');
  await submit(403);
  await expect(page.locator('[data-request-notice]:visible')).toHaveCount(1);
  expect(await oldForm.evaluate(node => node.isConnected)).toBe(true);
  expect(await matchingTasks(request, title)).toEqual([]);

  const login = await openSignIn(page, '#pk-auth-anonymous');
  try {
    await signIn(login);
  } finally {
    await login.close();
  }
  await page.bringToFront();
  // Whitespace passes native required validation but the real domain refuses
  // it. HTMX replaces #screen-form with the returned 422 form, not this notice.
  await field.fill('   ');
  await submit(422);
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(notice).toBeHidden();
  expect(await oldForm.evaluate(node => node.isConnected)).toBe(false);
  expect(await retainedNotice.evaluate(node => node.isConnected)).toBe(true);
  expect(await matchingTasks(request, title)).toEqual([]);

  await field.fill(title);
  expect((await page.request.post('/api/v1/auth/logout')).status()).toBe(200);
  await submit(403);
  await expect(notice).toBeVisible();
  await expect(field).toHaveValue(title);
  await expect(page.locator('[data-request-notice]:visible')).toHaveCount(1);
  expect(await page.locator('#screen-form').evaluate(node => node.nextElementSibling?.id)).toBe('pk-auth-anonymous');
  expect(await retainedNotice.evaluate(node => node.isConnected)).toBe(true);
  expect(await matchingTasks(request, title)).toEqual([]);
  await oldForm.dispose();
  await retainedNotice.dispose();
});

test('a committed write with a lost response is reported as uncertain and never replayed', async ({ page, request }) => {
  await page.goto(`${tasks}/new`);
  const title = 'The response was lost after this task committed';
  const field = page.getByRole('textbox', { name: 'Title', exact: true });
  await field.fill(title);
  let submissions = 0;
  let committedStatus = 0;
  await page.route(new URL(tasks, page.url()).href, async route => {
    submissions++;
    // This forwards the real request and receives its committed result. Only
    // delivery to the page is interrupted; no successful response is invented.
    committedStatus = (await route.fetch()).status();
    await route.abort('failed');
  });
  await field.press('Enter');
  await expect(page.locator('#pk-auth-uncertain').getByRole('alert')).toContainText('Check the result');
  expect(committedStatus).toBe(204);
  await expect(page.locator('#pk-auth-anonymous')).toBeHidden();
  await expect(field).toHaveValue(title);
  await expect(field).toBeFocused();
  await expect(page).toHaveURL(new URL(`${tasks}/new`, page.url()).href);
  expect(await matchingTasks(request, title)).toHaveLength(1);
  expect(submissions).toBe(1);
});

// These injected responses isolate controller classification. They do not
// claim that permissions, account changes or server outages were reproduced.
test('coded refusals and ambiguous responses keep input without an automatic retry', async ({ page, request }) => {
  const cases = [
    { name: 'denied', status: 403, body: '{"detail":"AUTH_DENIED: task:manage"}', notice: 'denied' },
    { name: 'changed', status: 403, body: '{"detail":"AUTH_PRINCIPAL_CHANGED: account changed"}', notice: 'changed' },
    { name: 'lookalike code', status: 403, body: '{"detail":"AUTH_ANONYMOUS_OTHER: not the code"}', notice: 'uncertain' },
    { name: 'malformed JSON', status: 403, body: '{', notice: 'uncertain' },
    { name: 'missing detail', status: 403, body: '{}', notice: 'uncertain' },
    { name: 'HTML', status: 403, body: '<p>AUTH_ANONYMOUS: not JSON</p>', notice: 'uncertain', contentType: 'text/html' },
    { name: 'server failure', status: 503, body: '{"detail":"AUTH_ANONYMOUS: not a 403"}', notice: 'uncertain' },
  ];
  for (const scenario of cases) {
    await page.goto(`${tasks}/new`);
    const title = `Controller refusal: ${scenario.name}`;
    const field = page.getByRole('textbox', { name: 'Title', exact: true });
    await field.fill(title);
    const endpoint = new URL(tasks, page.url()).href;
    let submissions = 0;
    await page.route(endpoint, async route => {
      submissions++;
      await route.fulfill({
        status: scenario.status,
        contentType: scenario.contentType ?? 'application/problem+json',
        body: scenario.body,
      });
    });
    await field.press('Enter');
    await expect(page.locator(`#pk-auth-${scenario.notice}`)).toBeVisible();
    for (const name of ['anonymous', 'denied', 'changed', 'uncertain']) {
      if (name !== scenario.notice) await expect(page.locator(`#pk-auth-${name}`)).toBeHidden();
    }
    await expect(field).toHaveValue(title);
    await expect(field).toBeFocused();
    expect(await matchingTasks(request, title)).toEqual([]);
    expect(submissions).toBe(1);
    await page.unroute(endpoint);
    // A completed ordinary save supplies the positive control and clears stale
    // notices. None of the injected responses is treated as write success.
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/task\/tasks\/[0-9a-f-]{36}$/);
    expect(await matchingTasks(request, title)).toHaveLength(1);
    for (const name of ['anonymous', 'denied', 'changed', 'uncertain']) {
      await expect(page.locator(`#pk-auth-${name}`)).toBeHidden();
    }
  }
});
