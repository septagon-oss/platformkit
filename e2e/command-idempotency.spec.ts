import { expect, test, type Browser, type Page } from '@playwright/test';

// The command controller, driven on the page that ships it.
//
// ui/assets/js/command.js is the kernel's first htmx extension: a form with
// hx-ext="command" mints one Idempotency-Key per submission, keeps the bytes it
// sent, and sends them again under that key when the transport failed. Every other
// gate for this feature asks the server what it would have done with a repeat — the
// nineteen cases in kit/httpx/idempotency_test.go drive the chain directly. This
// spec asks the only question those cases cannot: whether a browser mints a key at
// all, and what it does when the answer to a revocation never arrives.
//
// It matters which end of the connection forgot. A request that never reached the
// application ran nothing, and its retry is the first run; a request the application
// answered, whose answer then died, is the case the whole feature exists for, and the
// one a native form post cannot survive — the page is gone with the response, and so
// is the script that would have retried it. Both halves are written below, and they
// are separated by `route.fetch()`: the upstream request is made, the application
// revokes the session and records its answer, and only then is the response
// destroyed. A `route.abort()` alone would have proven less, because the retry would
// then be an ordinary first submission.
//
// The sessions screen is the page this opt-in lives on (modules/admin/internal/
// sessions.go), and one revocation is the command: it is the write in this
// application that a person cannot repeat safely by guessing, because the row they
// clicked disappears while they are deciding.
//
// A submission record lives in one document (ui/assets/js/command.js), so the second
// case below signs in a third device after the reload rather than reading the
// browser's memory: what a reload leaves behind is a fact a test can only read at the
// wire, and command-record-after-reload.spec.ts reads it there.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const revokeUrl = '**/app/auth/sessions/revoke';
const keyPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

async function signIn(browser: Browser): Promise<Page> {
  const page = await browser.newPage();
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
  return page;
}

const rows = (page: Page) => page.locator('tr[data-pk-row]');
const thisDevice = (page: Page) => rows(page).filter({ hasText: 'This device' });
const elsewhere = (page: Page) => rows(page).filter({ hasNotText: 'This device' });

// loseTheAnswer makes the first POST reach the application and lose its response.
// `sent` grows when a request arrives, `ran` when the application has answered the
// one whose response is destroyed, and `answered` when the browser has been answered:
// waiting on the first of those and acting on it is the difference between reloading
// after a lost response and reloading in the middle of one — the first version of this
// file did that, and under a loaded suite the revocation had not happened yet.
// `arrived` counts the requests the application saw at all.
async function loseTheAnswer(page: Page) {
  const sent: string[] = [];
  const answered: { status: number; replay: string | null; hxRedirect: string | null }[] = [];
  const ran: { status: number; replay: string | null }[] = [];
  let arrived = 0;
  await page.route(revokeUrl, async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    sent.push(route.request().headers()['idempotency-key'] ?? '');
    if (arrived === 0) {
      arrived++;
      const upstream = await route.fetch();
      ran.push({ status: upstream.status(), replay: upstream.headers()['idempotency-replay'] ?? null });
      return route.abort('connectionrefused');
    }
    arrived++;
    await route.fallback();
  });
  page.on('response', response => {
    if (response.url().endsWith('/app/auth/sessions/revoke')) {
      answered.push({ status: response.status(), replay: response.headers()['idempotency-replay'] ?? null,
        hxRedirect: response.headers()['hx-redirect'] ?? null });
    }
  });
  return { sent, ran, answered, arrived: () => arrived };
}

test('a revocation whose answer never arrived is sent again under the same key, and answered from the record', async ({ browser }) => {
  test.setTimeout(90_000);
  const here = await signIn(browser);
  const laptop = await signIn(browser);

  await here.goto('/app/auth/sessions');
  // Relative to what the screen says, for the reason auth-sessions.spec.ts gives:
  // every sign-in in this suite leaves a live session behind, and an absolute row
  // count would be a claim about the suite's order rather than about this command.
  const baseline = await rows(here).count();
  expect(baseline).toBeGreaterThanOrEqual(2);
  const row = elsewhere(here).first();
  const ref = await row.getAttribute('data-pk-row');
  expect(ref).toBeTruthy();

  const { sent, ran, answered, arrived } = await loseTheAnswer(here);
  await row.getByRole('button', { name: /^End the session on / }).click();

  // The retry is not the last attempt, so nothing is reported: the existing
  // "outcome is unknown" notice stays hidden, because an attempt is outstanding.
  await expect(here.locator('[data-request-notice]').first()).toBeHidden();

  // The controller's own promise, counted at the wire: two requests, one key, the
  // same lowercase UUID the first attempt used — and no third.
  await expect.poll(() => sent.length, { timeout: 15_000 }).toBe(2);
  await expect.poll(() => answered.length, { timeout: 15_000 }).toBe(1);
  expect(sent[0]).toMatch(keyPattern);
  expect(sent).toEqual([sent[0], sent[0]]);

  // The second request was answered from the record rather than by running the
  // command again. One entry, because the attempt whose answer was destroyed
  // answers the browser with nothing at all — that is the outcome this case is
  // about — and the one response that did arrive is the replay. The page the person
  // is left on is the list the revocation redrew: the same destination a delivered
  // answer would have taken them to.
  expect(ran).toEqual([{ status: 204, replay: null }]);
  expect(answered).toEqual([{ status: 204, replay: 'true', hxRedirect: '/app/auth/sessions' }]);
  await expect(here).toHaveURL(/\/app\/auth\/sessions$/);
  await expect(here.locator(`tr[data-pk-row="${ref}"]`)).toHaveCount(0);
  await expect(rows(here)).toHaveCount(baseline - 1);

  // One revocation, and it is the one the person clicked: the reading session is
  // still a session, and the machine whose row was clicked is not. With one other
  // row on the screen there is nothing else the click can have ended, which is why
  // the suite signs in from exactly two places — with three, "which row is which
  // machine" is a question about last_seen_at rather than about this command.
  expect(arrived()).toBe(2);
  await laptop.goto('/app/auth/sessions');
  await expect(laptop).toHaveURL(/\/app\/admin\/login/);
  await here.goto('/app/auth/sessions');
  await expect(here.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
});

test('a reload after a lost answer finds the command done, and mints no second key', async ({ browser }) => {
  test.setTimeout(90_000);
  const here = await signIn(browser);
  const laptop = await signIn(browser);

  await here.goto('/app/auth/sessions');
  const baseline = await rows(here).count();
  expect(baseline).toBeGreaterThanOrEqual(2);
  const ref = await elsewhere(here).first().getAttribute('data-pk-row');

  const { sent, ran, arrived } = await loseTheAnswer(here);
  await here.locator(`tr[data-pk-row="${ref}"]`)
    .getByRole('button', { name: /^End the session on / }).click();

  // Wait for the application's answer, then for the answer to be destroyed — and
  // only then reload, before the controller's first backoff elapses. This is the
  // person who got bored, not the retry: no second request exists to make the
  // promise for them, and everything after the reload is a new document.
  await expect.poll(() => ran.length, { timeout: 10_000 }).toBe(1);
  expect(ran).toEqual([{ status: 204, replay: null }]);
  await here.reload();

  // The list says the revocation happened — the outcome was lost, not the command —
  // and the browser was never asked to trust a fresh attempt: the key it had already
  // minted is the one the application saw, and it is the only one that ever existed.
  await expect(here.locator(`tr[data-pk-row="${ref}"]`)).toHaveCount(0);
  await expect(rows(here)).toHaveCount(baseline - 1);
  expect(arrived()).toBe(1);
  expect(sent).toEqual([sent[0]]);

  // Nothing is remembered across the reload, which is the controller's promise and
  // not a shortfall: a page redrawn from the server cannot tell the person retrying
  // the submission whose answer was lost from the person asking for something new,
  // because the two are the same bytes. So the record died with the document that
  // wrote it, and the next deliberate command is a new one. A third device signs in
  // here for exactly that test: the key the application sees now is not the key it
  // saw before the reload, and the revocation runs rather than being remembered.
  const phone = await signIn(browser);
  await phone.goto('/app/auth/sessions');
  const phoneRef = await thisDevice(phone).first().getAttribute('data-pk-row');
  expect(phoneRef).toBeTruthy();
  await here.reload();
  await expect(here.locator(`tr[data-pk-row="${phoneRef}"]`)).toHaveCount(1);
  await here.locator(`tr[data-pk-row="${phoneRef}"]`)
    .getByRole('button', { name: /^End the session on / }).click();

  await expect.poll(() => sent.length, { timeout: 15_000 }).toBe(2);
  expect(sent[1]).toMatch(keyPattern);
  expect(sent[1]).not.toBe(sent[0]);
  await expect(here.locator(`tr[data-pk-row="${phoneRef}"]`)).toHaveCount(0);
  expect(arrived()).toBe(2);
  await phone.goto('/app/auth/sessions');
  await expect(phone).toHaveURL(/\/app\/admin\/login/);

  await laptop.goto('/app/auth/sessions');
  await expect(laptop).toHaveURL(/\/app\/admin\/login/);
});
