import { expect, test } from '@playwright/test';

// A sign-up leaves two things behind: the message in a mailbox, and a row that says
// the message left. This journey reads the row over the door the shell asks —
// POST /mail-delivery, keyed by the X-Request-ID the sign-up call was already
// answered with — and reads the message out of the catcher beside it.
//
// What it can prove, and what it cannot, are both worth stating. It proves that the
// door answers a stranger's own call with one field and the word `pending`: nothing
// was refused. It proves the same answer for the address that has an account and the
// one that does not (modules/auth/internal/mail_delivery_neutral_test.go, at the
// route), and it proves no credential crosses the socket. What it cannot prove is
// that the row says `sent` — the door never says that to anybody, because a route
// that mails only addresses where somebody has an account cannot report that a mail
// left without reporting who has an account. The row's own `sent` is read in
// modules/auth/internal/mail_delivery_test.go against the table, and the shell's
// reading of a `failed` one in e2e/email-verification-forms.spec.ts.
//
// Reading the catcher first and the door second is the order that matters: the mail
// and its record commit together, so a message already in Mailpit is a send whose
// row is at worst a commit away. The door's budget is its own
// (contracts.MailDeliveryAsks per address per window, one address here for the whole
// run), so this asks it a handful of times rather than in a loop that spins. See
// e2e/mailed-links.spec.ts for the same mailbox read at link length.

const mailpit = process.env.PLATFORMKIT_E2E_MAILPIT_URL ?? 'http://localhost:8025';

// How many messages the catcher holds for one address. An empty count is what "not yet"
// looks like, so the caller can wait on it.
async function held(request: import('@playwright/test').APIRequestContext, to: string): Promise<number> {
  const found = await request
    .get(`${mailpit}/api/v1/search`, { params: { query: `to:"${to}"` } })
    .catch(() => null);
  if (!found || !found.ok()) return 0;
  const { messages } = (await found.json()) as { messages?: unknown[] };
  return (messages ?? []).length;
}

// The one-time credential the mailed link carries, for the assertion that it is the
// only place it appears on this side of the socket.
async function credential(request: import('@playwright/test').APIRequestContext, to: string): Promise<string> {
  const found = await request.get(`${mailpit}/api/v1/search`, { params: { query: `to:"${to}"` } });
  expect(found.ok(), await found.text()).toBe(true);
  const { messages } = (await found.json()) as { messages?: { ID: string }[] };
  expect(messages?.length, `the catcher holds a message for ${to}`).toBe(1);
  const detail = await request.get(`${mailpit}/api/v1/message/${messages![0].ID}`);
  expect(detail.ok(), await detail.text()).toBe(true);
  const { Text, HTML } = (await detail.json()) as { Text?: string; HTML?: string };
  const link = `${Text ?? ''}\n${HTML ?? ''}`.match(/\/app\/auth\/verify-email\?token=([A-Za-z0-9_\-]+)/);
  expect(link, 'the confirmation carries its credential').not.toBeNull();
  return link![1];
}

test('the mail one sign-up caused leaves a record, and the door says only that nothing was refused', async ({ page }) => {
  // The record is written when the worker that mails the link runs, not when the
  // sign-up is accepted, and the catcher is what says when that has happened.
  test.setTimeout(120_000);

  const stranger = {
    email: `delivery-record-${Date.now()}@e2e.test`,
    password: 'e2e-delivery-record-password',
    name: 'Delivery Record Stranger',
  };

  // A stranger, in a browser holding no session, at the page that asks for an account.
  await page.goto('/app/admin/register');
  await page.locator('input[name="email"]').fill(stranger.email);
  await page.locator('input[name="displayName"]').fill(stranger.name);
  await page.locator('input[name="password"]').fill(stranger.password);
  await page.locator('input[name="confirmation"]').fill(stranger.password);
  // The consent box is the design system's own: the native input is invisible behind a
  // painted box, so the keyboard is the path that reaches it from any copy.
  const box = page.locator('input[name="termsAccepted"]');
  await box.focus();
  await page.keyboard.press('Space');
  await expect(box).toBeChecked();

  const registered = page.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/register'));
  await page.locator('form[data-auth-form="register-password"] button[type="submit"]').click();
  const accepted = await registered;
  expect(accepted.status(), 'the sign-up was not accepted').toBe(202);

  // The handle the door is asked with is the one this call was already answered with
  // (kit/httpx/request_id.go): no new token, no response field, no correlation table.
  const requestID = accepted.headers()['x-request-id'];
  expect(requestID, 'the answer to the sign-up names the call').toBeTruthy();
  const door = new URL(accepted.url()).pathname.replace(/\/register$/, '/mail-delivery');

  // The acknowledgment itself is not read here: it is the same neutral sentence
  // whether or not a mail left (e2e/mailed-links.spec.ts reads it), its body is gone
  // once the shell navigates on the 202, and the one field this journey cares about
  // — `state` — belongs to the door below and appears nowhere else.

  await expect
    .poll(async () => held(page.request, stranger.email), {
      message: `Mailpit at ${mailpit} holds the sign-up confirmation for ${stranger.email}`,
      timeout: 45_000,
    })
    .toBe(1);
  const token = await credential(page.request, stranger.email);

  // One ask, from the page itself, so it carries the origin and cookies the door asks
  // for (httpx.SameSite) and the tenant the host resolves to. The body names the
  // caller's own call and nothing else — no address, which is the whole point.
  const ask = async () => page.evaluate(async ({ path, requestId }) => {
    const answered = await fetch(path, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ requestId }),
      credentials: 'same-origin',
    });
    return { status: answered.status, text: await answered.text() };
  }, { path: door, requestId: requestID });
  // The row commits with the send it describes, so the message is already in the
  // catcher and this may still be a commit away. Three asks, spread over five
  // seconds, are more than the gap and a twentieth of this address's budget; the
  // loop only ends early on an answer that is not the `pending` it should be, so a
  // door that jumped the gun and said `failed` is seen, not waited out.
  let answer = await ask();
  for (const wait of [1000, 2000, 4000]) {
    if (!answer.text.startsWith('{')) break;
    const said = String((JSON.parse(answer.text) as { state?: string }).state ?? '');
    if (said !== '' && said !== 'pending') break;
    await page.waitForTimeout(wait);
    answer = await ask();
  }

  expect(answer.status, `the door answered: ${answer.text}`).toBe(200);
  const body = JSON.parse(answer.text) as Record<string, unknown>;
  // huma answers a JSON body with a link to that body's schema; it names nothing
  // about the mail, and everything below is about what is beside it.
  delete body.$schema;
  // One field, and the one word a door that mails only accounts may say about a
  // mail that went out: nothing was refused. `sent` would be the same sentence with
  // an address in it, and `failed` here would be a lie — the catcher is holding the
  // message this call caused.
  expect(Object.keys(body), `the door says no more than one field: ${answer.text}`).toEqual(['state']);
  expect(body.state, `the delivery record for ${requestID}`).toBe('pending');
  // No secret on the socket: the credential is in the message and in no row, and the
  // door reads that row.
  expect(answer.text).not.toContain(token);
});
